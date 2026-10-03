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
// along with Proton Mail Bridge. If not, see <https://www.gnu.org/licenses/>.

package dialer

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ProtonMail/go-proton-api"
	"github.com/ProtonMail/go-proton-api/server"
	"github.com/ProtonMail/proton-bridge/v3/internal/useragent"
	a "github.com/stretchr/testify/assert"
	r "github.com/stretchr/testify/require"
)

func getRootURL() string {
	return "https://mail-api.proton.me"
}

func TestTLSPinValid(t *testing.T) {
	called, _, _, _, cm := createClientWithPinningDialer(t, getRootURL())

	_, _, _ = cm.NewClientWithLogin(context.Background(), "username", []byte("password")) //nolint:dogsled

	checkTLSIssueHandler(t, 0, called)
}

func TestTLSPinBackup(t *testing.T) {
	called, _, _, checker, cm := createClientWithPinningDialer(t, getRootURL())
	copyTrustedPins(checker)
	checker.trustedPins[1] = checker.trustedPins[0]
	checker.trustedPins[0] = ""

	_, _, _ = cm.NewClientWithLogin(context.Background(), "username", []byte("password")) //nolint:dogsled

	checkTLSIssueHandler(t, 0, called)
}

func TestTLSPinInvalid(t *testing.T) {
	s := server.New()
	defer s.Close()

	called, _, _, _, cm := createClientWithPinningDialer(t, s.GetHostURL())

	_, _, _ = cm.NewClientWithLogin(context.Background(), "username", []byte("password")) //nolint:dogsled

	checkTLSIssueHandler(t, 1, called)
}

func TestTLSPinNoMatch(t *testing.T) {
	skipIfProxyIsSet(t)

	called, _, reporter, checker, cm := createClientWithPinningDialer(t, getRootURL())

	copyTrustedPins(checker)
	for i := 0; i < len(checker.trustedPins); i++ {
		checker.trustedPins[i] = "testing"
	}

	_, _, _ = cm.NewClientWithLogin(context.Background(), "username", []byte("password")) //nolint:dogsled
	_, _, _ = cm.NewClientWithLogin(context.Background(), "username", []byte("password")) //nolint:dogsled

	// Check that it will be reported only once per session, but notified every time.
	r.Equal(t, 1, len(reporter.sentReports))
	checkTLSIssueHandler(t, 2, called)
}

func TestTLSSignedCertWrongPublicKey(t *testing.T) {
	dialer, address := localPinningDialer(t, true, false)
	_, err := dialer.DialTLSContext(context.Background(), "tcp", address)
	r.ErrorIs(t, err, ErrTLSMismatch)
}

func TestTLSSignedCertTrustedPublicKey(t *testing.T) {
	dialer, address := localPinningDialer(t, true, true)
	conn, err := dialer.DialTLSContext(context.Background(), "tcp", address)
	r.NoError(t, err)
	defer conn.Close() //nolint:errcheck
	r.NotEmpty(t, conn.(*tls.Conn).ConnectionState().VerifiedChains)
}

func TestTLSSelfSignedCertTrustedPublicKey(t *testing.T) {
	dialer, address := localPinningDialer(t, false, true)
	conn, err := dialer.DialTLSContext(context.Background(), "tcp", address)
	r.NoError(t, err)
	defer conn.Close() //nolint:errcheck
	r.Empty(t, conn.(*tls.Conn).ConnectionState().VerifiedChains)
}

func TestTLSSelfSignedCertWrongPublicKey(t *testing.T) {
	dialer, address := localPinningDialer(t, false, false)
	_, err := dialer.DialTLSContext(context.Background(), "tcp", address)
	r.ErrorIs(t, err, ErrTLSMismatch)
}

type localTLSDialer struct{ *tls.Dialer }

func (d localTLSDialer) DialTLSContext(ctx context.Context, network, address string) (net.Conn, error) {
	return d.DialContext(ctx, network, address)
}

func (d localTLSDialer) ShouldSkipCertificateChainVerification(string) bool {
	return d.Config.InsecureSkipVerify
}

func localPinningDialer(t *testing.T, verifyChain, trustPin bool) (*PinningTLSDialer, string) {
	t.Helper()
	s := httptest.NewTLSServer(http.NotFoundHandler())
	t.Cleanup(func() {
		s.CloseClientConnections()
		s.Close()
	})
	config := s.Client().Transport.(*http.Transport).TLSClientConfig.Clone()
	config.InsecureSkipVerify = !verifyChain //nolint:gosec
	if !verifyChain {
		config.RootCAs = nil
	}
	pins := append([]string(nil), TrustedAPIPins...)
	if trustPin {
		hash := sha256.Sum256(s.Certificate().RawSubjectPublicKeyInfo)
		pins = append(pins, fmt.Sprintf(`pin-sha256=%q`, base64.StdEncoding.EncodeToString(hash[:])))
	}
	dialer := NewPinningTLSDialer(localTLSDialer{&tls.Dialer{Config: config}}, nil, NewTLSPinChecker(pins))
	return dialer, s.Listener.Addr().String()
}

func createClientWithPinningDialer(t *testing.T, hostURL string) (*atomicUint64, *PinningTLSDialer, *TLSReporter, *TLSPinChecker, *proton.Manager) {
	t.Helper()
	called := &atomicUint64{}

	reporter := NewTLSReporter(hostURL, "appVersion", useragent.New(), TrustedAPIPins)
	checker := NewTLSPinChecker(TrustedAPIPins)
	dialer := NewPinningTLSDialer(NewBasicTLSDialer(hostURL), reporter, checker)

	stop, done := make(chan struct{}), make(chan struct{})
	t.Cleanup(func() { close(stop); <-done })
	go func() {
		defer close(done)
		for {
			select {
			case <-dialer.GetTLSIssueCh():
				called.add(1)
			case <-stop:
				return
			}
		}
	}()

	return called, dialer, reporter, checker, proton.New(
		proton.WithHostURL(hostURL),
		proton.WithTransport(CreateTransportWithDialer(dialer)),
	)
}

func copyTrustedPins(pinChecker *TLSPinChecker) {
	copiedPins := make([]string, len(pinChecker.trustedPins))
	copy(copiedPins, pinChecker.trustedPins)
	pinChecker.trustedPins = copiedPins
}

func checkTLSIssueHandler(t *testing.T, wantCalledAtLeast uint64, called *atomicUint64) {
	// TLSIssueHandler is called in goroutine se we need to wait a bit to be sure it was called.
	a.Eventually(
		t,
		func() bool {
			if wantCalledAtLeast == 0 {
				return called.load() == 0
			}
			// Dialer can do more attempts resulting in more calls.
			return called.load() >= wantCalledAtLeast
		},
		time.Second,
		10*time.Millisecond,
	)
	// Repeated again so it generates nice message.
	if wantCalledAtLeast == 0 {
		r.Equal(t, uint64(0), called.load())
	} else {
		r.GreaterOrEqual(t, called.load(), wantCalledAtLeast)
	}
}

type atomicUint64 struct {
	v uint64
}

func (x *atomicUint64) load() uint64 { return atomic.LoadUint64(&x.v) }

func (x *atomicUint64) add(delta uint64) uint64 { return atomic.AddUint64(&x.v, delta) }
