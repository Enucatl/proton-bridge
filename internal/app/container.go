//go:build container

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

package app

import (
	"crypto/tls"
	"encoding/pem"
	"errors"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/Masterminds/semver/v3"
	"github.com/ProtonMail/proton-bridge/v3/internal/bridge"
	"github.com/ProtonMail/proton-bridge/v3/internal/constants"
	"github.com/ProtonMail/proton-bridge/v3/internal/cookies"
	"github.com/ProtonMail/proton-bridge/v3/internal/crash"
	"github.com/ProtonMail/proton-bridge/v3/internal/events"
	bridgeCLI "github.com/ProtonMail/proton-bridge/v3/internal/frontend/cli"
	"github.com/ProtonMail/proton-bridge/v3/internal/locations"
	"github.com/ProtonMail/proton-bridge/v3/internal/logging"
	"github.com/ProtonMail/proton-bridge/v3/internal/sentry"
	"github.com/ProtonMail/proton-bridge/v3/internal/services/observability"
	"github.com/ProtonMail/proton-bridge/v3/internal/useragent"
	"github.com/ProtonMail/proton-bridge/v3/internal/vault"
	"github.com/ProtonMail/proton-bridge/v3/pkg/keychain"
	"github.com/ProtonMail/proton-bridge/v3/pkg/restarter"
	"github.com/allan-simon/go-singleinstance"
	"github.com/sirupsen/logrus"
	"github.com/urfave/cli/v2"
)

const (
	flagLogIMAP = "log-imap"
	flagLogSMTP = "log-smtp"
)

func New() *cli.App {
	return &cli.App{
		Name:        "proton-bridge-headless",
		Usage:       "Proton Mail IMAP and SMTP service",
		HideVersion: true,
		Flags: []cli.Flag{
			&cli.BoolFlag{Name: "noninteractive", Aliases: []string{"n"}, Usage: "Run the mail service (default)"},
			&cli.BoolFlag{Name: "cli", Aliases: []string{"c"}, Usage: "Open the provisioning CLI"},
			&cli.BoolFlag{Name: "healthcheck", Usage: "Check both TLS protocol greetings"},
			&cli.BoolFlag{Name: "version", Aliases: []string{"v"}, Usage: "Print upstream and distribution versions"},
			&cli.StringFlag{Name: "data-dir", Value: "/data", Usage: "Private persistent state directory"},
			&cli.StringFlag{Name: "vault-key-file", Usage: "Existing 32-byte vault key secret (read-only; never generated or copied into state)"},
			&cli.StringFlag{Name: "tls-cert", Value: "/protonmail/certs/cert.pem", Usage: "Mounted PEM certificate chain"},
			&cli.StringFlag{Name: "tls-key", Value: "/protonmail/certs/key.pem", Usage: "Mounted PEM private key"},
			&cli.StringFlag{Name: "tls-server-name", Usage: "Healthcheck certificate hostname (defaults to the first certificate SAN)"},
			&cli.StringFlag{Name: "log-level", Aliases: []string{"l"}, Value: "info"},
			&cli.StringFlag{Name: flagLogIMAP, Usage: "Log decrypted IMAP traffic (client|server|all)"},
			&cli.BoolFlag{Name: flagLogSMTP, Usage: "Log decrypted SMTP traffic"},
		},
		Action: runContainer,
	}
}

func runContainer(c *cli.Context) error {
	if c.Bool("version") {
		fmt.Printf("proton-bridge-headless %s (upstream %s, revision %s)\n", constants.DownstreamVersion, constants.Version, constants.Revision)
		return nil
	}
	if c.Bool("cli") && (c.Bool("noninteractive") || c.Bool("healthcheck")) {
		return fmt.Errorf("--cli cannot be combined with --noninteractive or --healthcheck")
	}
	if c.Bool("healthcheck") {
		return healthcheck(c)
	}
	certPath, keyPath := configuredCertificatePaths(c)
	if certPath != "" || keyPath != "" || c.IsSet("tls-cert") || c.IsSet("tls-key") {
		pair, err := tls.LoadX509KeyPair(certPath, keyPath)
		if err != nil {
			return fmt.Errorf("load configured TLS certificate: %w", err)
		}
		if _, err := certificateLeaf(pair.Certificate[0]); err != nil {
			return err
		}
	}
	ctx, stop := signal.NotifyContext(c.Context, syscall.SIGTERM, syscall.SIGINT)
	defer stop()
	c.Context = ctx
	version, err := semver.NewVersion(constants.Version)
	if err != nil {
		return err
	}
	dataDir, err := filepath.Abs(c.String("data-dir"))
	if err != nil {
		return err
	}
	provider := containerProvider{root: dataDir}
	for _, dir := range []string{dataDir, provider.UserConfig(), provider.UserData(), provider.UserCache()} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return err
		}
	}
	loc := locations.New(provider, constants.ConfigName)
	lock, err := singleinstance.CreateLockFile(loc.GetLockFile())
	if err != nil {
		return fmt.Errorf("lock state directory: %w", err)
	}
	defer lock.Close()
	panicErr := make(chan error, 1)
	crashHandler := crash.NewHandler(func(value any) error {
		logrus.WithField("panic", value).Error("Bridge panicked")
		select {
		case panicErr <- fmt.Errorf("bridge panic: %v", value):
		default:
		}
		stop()
		return nil
	})
	logs, err := loc.ProvideLogsPath()
	if err != nil {
		return err
	}
	sessionID := logging.NewSessionID()
	closer, err := logging.Init(logs, sessionID, logging.BridgeShortAppName, logging.DefaultMaxLogFileSize, logging.DefaultPruningSize, c.String("log-level"))
	if err != nil {
		return err
	}
	defer func() { _ = logging.Close(closer) }()
	crashHandler.AddRecoveryAction(logging.DumpStackTrace(logs, sessionID, "bridge"))
	settings, err := loc.ProvideSettingsPath()
	if err != nil {
		return err
	}
	var key []byte
	if c.IsSet("vault-key-file") {
		key, err = vault.LoadSecretKey(c.String("vault-key-file"))
	} else {
		key, err = vault.LoadFileKey(filepath.Join(dataDir, "vault.key"), settings)
	}
	if err != nil {
		return err
	}
	cache, err := loc.ProvideGluonCachePath()
	if err != nil {
		return err
	}
	v, _, err := vault.New(settings, cache, key, crashHandler)
	if err != nil {
		return err
	}
	// Serialized desktop preferences stay compatible; the service policy is fixed.
	for _, set := range []func() error{
		func() error { return v.SetBridgeTLSCertPath(certPath, keyPath) },
		func() error { return v.SetIMAPPort(1143) },
		func() error { return v.SetSMTPPort(1025) },
		func() error { return v.SetIMAPSSL(true) },
		func() error { return v.SetSMTPSSL(true) },
		func() error { return v.SetAutoUpdate(false) },
	} {
		if err := set(); err != nil {
			return err
		}
	}
	certPEM, keyPEM := v.GetBridgeTLSCert()
	pair, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return fmt.Errorf("load active TLS certificate: %w", err)
	}
	if _, err := certificateLeaf(pair.Certificate[0]); err != nil {
		return err
	}
	// Export only the public certificate for client trust and credential-free healthchecks.
	var publicPEM []byte
	for _, der := range pair.Certificate {
		publicPEM = append(publicPEM, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})...)
	}
	if err := os.WriteFile(filepath.Join(dataDir, "tls-cert.pem"), publicPEM, 0o600); err != nil {
		return err
	}
	baseJar, err := cookiejar.New(nil)
	if err != nil {
		return err
	}
	jar, err := cookies.NewCookieJar(baseJar, v)
	if err != nil {
		return err
	}
	apiURL, err := url.Parse(constants.APIHost)
	if err != nil {
		return err
	}
	for name, value := range map[string]string{"hhn": sentry.GetProtectedHostname(), "tz": sentry.GetTimeZone(), "lng": sentry.GetSystemLang(), "clr": "light"} {
		jar.SetCookies(apiURL, []*http.Cookie{{Name: name, Value: value, Secure: true}})
	}
	defer func() {
		if err := jar.PersistCookies(); err != nil {
			logrus.WithError(err).Error("Persist cookies")
		}
	}()
	identifier := useragent.New()
	return observability.WithObservability(loc, func(obs *observability.Service) error {
		return withBridge(c, "", loc, version, identifier, obs, crashHandler, sentry.NullSentryReporter{}, v, jar, keychain.NewList(), func(b *bridge.Bridge, eventCh <-chan events.Event) error {
			var frontendErr error
			if c.Bool("cli") && ctx.Err() == nil {
				frontendErr = bridgeCLI.New(b, restarter.New(""), eventCh, crashHandler, ctx.Done()).Loop()
			} else {
				<-ctx.Done()
			}
			select {
			case err := <-panicErr:
				return err
			default:
				return frontendErr
			}
		})
	})
}

// Default mount paths are optional; an explicit or partially present pair must validate.
func configuredCertificatePaths(c *cli.Context) (string, string) {
	certPath, keyPath := c.String("tls-cert"), c.String("tls-key")
	if c.IsSet("tls-cert") || c.IsSet("tls-key") {
		return certPath, keyPath
	}
	_, certErr := os.Lstat(certPath)
	_, keyErr := os.Lstat(keyPath)
	if errors.Is(certErr, os.ErrNotExist) && errors.Is(keyErr, os.ErrNotExist) {
		return "", ""
	}
	return certPath, keyPath
}

type containerProvider struct{ root string }

func (p containerProvider) UserConfig() string {
	return filepath.Join(p.root, "config", constants.VendorName, constants.ConfigName)
}
func (p containerProvider) UserData() string {
	return filepath.Join(p.root, "data", constants.VendorName, constants.ConfigName)
}
func (p containerProvider) UserCache() string {
	return filepath.Join(p.root, "cache", constants.VendorName, constants.ConfigName)
}

type containerAutostarter struct{}

func (containerAutostarter) Enable() error {
	return fmt.Errorf("startup is managed by the container runtime")
}
func (containerAutostarter) Disable() error                   { return nil }
func (containerAutostarter) IsEnabled() bool                  { return false }
func newAutostarter(string) bridge.Autostarter                { return containerAutostarter{} }
func newUpdater(*locations.Locations) (bridge.Updater, error) { return nil, nil }
