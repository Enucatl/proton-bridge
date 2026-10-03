// Copyright (c) 2026 Proton AG
//
// This file is part of Proton Mail Bridge.
//
// Proton Mail Bridge is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// Proton Mail Bridge is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU General Public License for more details.
//
// You should have received a copy of the GNU General Public License
// along with Proton Mail Bridge.  If not, see <https://www.gnu.org/licenses/>.

package tests

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/ProtonMail/gluon/async"
	"github.com/ProtonMail/gluon/imap"
	"github.com/ProtonMail/proton-bridge/v3/internal/bridge"
	"github.com/ProtonMail/proton-bridge/v3/internal/constants"
	"github.com/ProtonMail/proton-bridge/v3/internal/cookies"
	"github.com/ProtonMail/proton-bridge/v3/internal/dialer"
	"github.com/ProtonMail/proton-bridge/v3/internal/events"
	frontend "github.com/ProtonMail/proton-bridge/v3/internal/frontend/grpc"
	"github.com/ProtonMail/proton-bridge/v3/internal/service"
	"github.com/ProtonMail/proton-bridge/v3/internal/services/observability"
	"github.com/ProtonMail/proton-bridge/v3/internal/useragent"
	"github.com/ProtonMail/proton-bridge/v3/internal/vault"
	"github.com/ProtonMail/proton-bridge/v3/pkg/keychain"
	"github.com/sirupsen/logrus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/types/known/emptypb"
)

func (t *testCtx) startBridge() error {
	logrus.Info("Starting bridge")

	eventCh, err := t.initBridge()
	if err != nil {
		return fmt.Errorf("could not create bridge: %w", err)
	}

	logrus.Info("Starting frontend service")

	if err := t.initFrontendService(eventCh); err != nil {
		return fmt.Errorf("could not create frontend service: %w", err)
	}

	logrus.Info("Starting frontend client")

	if err := t.initFrontendClient(); err != nil {
		return fmt.Errorf("could not create frontend client: %w", err)
	}

	t.events.await(events.AllUsersLoaded{}, 30*time.Second)

	return nil
}

func (t *testCtx) stopBridge() error {
	return errors.Join(
		t.closeFrontendService(context.Background()),
		t.closeFrontendClient(),
		t.closeBridge(context.Background()),
	)
}

func (t *testCtx) initBridge() (<-chan events.Event, error) {
	if t.bridge != nil {
		return nil, fmt.Errorf("bridge is already started")
	}

	// Bridge will disable the proxy by default at startup.
	t.mocks.ProxyCtl.EXPECT().DisallowProxy()

	// Get the path to the vault.
	vaultDir, err := t.locator.ProvideSettingsPath()
	if err != nil {
		return nil, fmt.Errorf("could not get vault dir: %w", err)
	}

	// Get the default gluon path.
	gluonCacheDir, err := t.locator.ProvideGluonDataPath()
	if err != nil {
		return nil, fmt.Errorf("could not get gluon dir: %w", err)
	}

	// Create the vault.
	vault, corrupt, err := vault.New(vaultDir, gluonCacheDir, t.storeKey, async.NoopPanicHandler{})
	if err != nil {
		return nil, fmt.Errorf("could not create vault: %w", err)
	} else if corrupt != nil {
		return nil, fmt.Errorf("vault is corrupt: %w", corrupt)
	}
	t.vault = vault

	// Create the underlying cookie jar.
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, fmt.Errorf("could not create cookie jar: %w", err)
	}

	// Create the persisting cookie jar.
	persister, err := cookies.NewCookieJar(jar, vault)
	if err != nil {
		return nil, fmt.Errorf("could not create cookie persister: %w", err)
	}

	var (
		logIMAP bool
		logSMTP bool
	)

	if len(os.Getenv("FEATURE_TEST_LOG_IMAP")) != 0 {
		logIMAP = true
	}

	if len(os.Getenv("FEATURE_TEST_LOG_SMTP")) != 0 {
		logSMTP = true
	}

	if logIMAP || logSMTP {
		logrus.SetLevel(logrus.TraceLevel)
	}

	rt := t.netCtl.NewRoundTripper(&tls.Config{InsecureSkipVerify: true})

	// We store the round tripper in the testing context so we can cancel the connection
	// when we're turning it down/up
	t.rt = &rt

	if isBlack() {
		// GODT-1602 make sure we don't time out test server
		t, ok := rt.(*http.Transport)
		if !ok {
			panic("expecting http.Transport")
		}
		dialer.SetBasicTransportTimeouts(t)
	}

	// Create the bridge.
	bridge, eventCh, err := bridge.New(
		// App stuff
		t.locator,
		vault,
		t.mocks.Autostarter,
		t.mocks.Updater,
		t.version,
		keychain.NewTestKeychainsList(),
		observability.NewTestService(),

		// API stuff
		t.api.GetHostURL(),
		persister,
		useragent.New(),
		t.mocks.TLSReporter,
		rt,
		t.mocks.ProxyCtl,
		t.mocks.CrashHandler,
		t.reporter,
		imap.DefaultEpochUIDValidityGenerator(),
		t.heartbeat,

		// Logging stuff
		logIMAP,
		logIMAP,
		logSMTP,
	)
	if err != nil {
		return nil, fmt.Errorf("could not create bridge: %w", err)
	}

	t.bridge = bridge
	t.heartbeat.setBridge(bridge)

	return t.events.collectFrom(eventCh), nil
}

func (t *testCtx) closeBridge(ctx context.Context) error {
	if t.bridge == nil {
		return fmt.Errorf("bridge is not started")
	}

	t.bridge.Close(ctx)

	t.bridge = nil

	return nil
}

func (t *testCtx) initFrontendService(eventCh <-chan events.Event) error {
	if t.service != nil {
		return fmt.Errorf("frontend service is already started")
	}

	// When starting the frontend, we might enable autostart on bridge if it isn't already.
	t.mocks.Autostarter.EXPECT().Enable().AnyTimes()
	t.mocks.Autostarter.EXPECT().IsEnabled().AnyTimes()

	stopCh := make(chan struct{})
	service, err := frontend.NewService(
		&async.NoopPanicHandler{},
		new(mockRestarter),
		t.locator,
		t.bridge,
		eventCh,
		stopCh,
		true,
		-1,
	)
	if err != nil {
		return fmt.Errorf("could not create service: %w", err)
	}

	logrus.Info("Frontend service started")

	t.service = service
	t.serviceStop = stopCh
	t.serviceErr = nil

	t.serviceWG.Go(func() {
		t.serviceErr = service.Loop()
	})

	return nil
}

func (t *testCtx) closeFrontendService(ctx context.Context) error {
	if t.service == nil {
		return fmt.Errorf("frontend service is not started")
	}

	var quitErr error
	if t.client != nil {
		_, quitErr = t.client.Quit(ctx, &emptypb.Empty{})
	}
	if t.client == nil || quitErr != nil {
		close(t.serviceStop)
	}
	t.serviceWG.Wait()

	logrus.Info("Frontend service stopped")

	t.service = nil
	t.serviceStop = nil

	return errors.Join(quitErr, t.serviceErr)
}

func (t *testCtx) initFrontendClient() error {
	if t.client != nil {
		return fmt.Errorf("frontend client is already started")
	}

	settings, err := t.locator.ProvideSettingsPath()
	if err != nil {
		return fmt.Errorf("could not get settings path: %w", err)
	}

	b, err := os.ReadFile(filepath.Join(settings, "grpcServerConfig.json"))
	if err != nil {
		return fmt.Errorf("could not read grpcServerConfig.json: %w", err)
	}

	var cfg service.Config

	if err := json.Unmarshal(b, &cfg); err != nil {
		return fmt.Errorf("could not unmarshal grpcServerConfig.json: %w", err)
	}

	cp := x509.NewCertPool()

	if !cp.AppendCertsFromPEM([]byte(cfg.Cert)) {
		return fmt.Errorf("failed to append certificates to pool")
	}

	var target string
	if len(cfg.FileSocketPath) != 0 {
		target = "unix://" + cfg.FileSocketPath
	} else {
		target = fmt.Sprintf("%v:%d", constants.Host, cfg.Port)
	}

	conn, err := grpc.NewClient(
		target,
		grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{RootCAs: cp, ServerName: "127.0.0.1"})),
		grpc.WithUnaryInterceptor(func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
			return invoker(metadata.AppendToOutgoingContext(ctx, "server-token", cfg.Token), method, req, reply, cc, opts...)
		}),
	)
	if err != nil {
		return fmt.Errorf("could not dial grpc server: %w", err)
	}

	client := frontend.NewBridgeClient(conn)

	stream, err := client.RunEventStream(context.Background(), &frontend.EventStreamRequest{ClientPlatform: runtime.GOOS})
	if err != nil {
		_ = conn.Close()
		return fmt.Errorf("could not start event stream: %w", err)
	}

	eventCh := async.NewQueuedChannel[*frontend.StreamEvent](0, 0, async.NoopPanicHandler{}, "test-frontend-client")

	t.clientWG.Go(func() {
		defer func() {
			eventCh.CloseAndDiscardQueued()
			eventCh.Wait()
		}()

		for {
			event, err := stream.Recv()
			if err != nil {
				return
			}

			eventCh.Enqueue(event)
		}
	})

	logrus.Info("Frontend client started")

	t.client = client
	t.clientConn = conn
	t.clientEventCh = eventCh

	return nil
}

func (t *testCtx) closeFrontendClient() error {
	if t.client == nil {
		return fmt.Errorf("frontend client is not started")
	}

	err := t.clientConn.Close()
	t.clientWG.Wait()

	logrus.Info("Frontend client stopped")

	t.client = nil
	t.clientConn = nil
	t.clientEventCh = nil

	return err
}
func (t *testCtx) expectProxyCtlAllowProxy() {
	t.mocks.ProxyCtl.EXPECT().AllowProxy()
}

type mockRestarter struct{}

func (m *mockRestarter) Set(_, _ bool) {}

func (m *mockRestarter) AddFlags(_ ...string) {}

func (m *mockRestarter) Override(_ string) {}
