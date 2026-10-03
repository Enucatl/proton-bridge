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
// along with Proton Mail Bridge. If not, see <https://www.gnu.org/licenses/>.

package imapsmtpserver

import (
	"crypto/tls"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"sync"

	"github.com/ProtonMail/proton-bridge/v3/internal/identifier"
	"strconv"
	"testing"
	"time"
)

func TestContainerListenerRequiresTLS(t *testing.T) {
	if listener, err := newListener(0, false, nil); err == nil {
		listener.Close()
		t.Fatal("plaintext listener was allowed")
	}
	fixture := httptest.NewTLSServer(nil)
	config := fixture.TLS.Clone()
	fixture.Close()
	listener, err := newListener(0, true, config)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	defer func() { listener.Close(); wg.Wait() }()
	addr, ok := listener.Addr().(*net.TCPAddr)
	if !ok || !addr.IP.IsUnspecified() {
		t.Fatalf("listener did not bind container interfaces: %v", listener.Addr())
	}

	// Sending plaintext credentials cannot reach any mail protocol handler.
	done := make(chan error, 1)
	wg.Go(func() {
		conn, err := listener.Accept()
		if err != nil {
			done <- err
			return
		}
		defer conn.Close()
		_ = conn.SetDeadline(time.Now().Add(time.Second))
		_, err = conn.Read(make([]byte, 1))
		done <- err
	})
	conn, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(addr.Port)), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := io.WriteString(conn, "LOGIN user password\r\n"); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err == nil {
		t.Fatal("TLS listener accepted plaintext credentials")
	}

	// A trusted TLS client can complete the shared listener handshake.
	wg.Go(func() {
		conn, err := listener.Accept()
		if err != nil {
			done <- err
			return
		}
		defer conn.Close()
		_ = conn.SetDeadline(time.Now().Add(time.Second))
		done <- conn.(*tls.Conn).Handshake()
	})
	clientConfig := fixture.Client().Transport.(*http.Transport).TLSClientConfig.Clone()
	client, err := tls.DialWithDialer(&net.Dialer{Timeout: time.Second}, "tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(addr.Port)), clientConfig)
	if err != nil {
		t.Fatal(err)
	}
	client.Close()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

type plaintextSMTPSettings struct{ SMTPSettingsProvider }

func (plaintextSMTPSettings) TLSConfig() *tls.Config                  { return nil }
func (plaintextSMTPSettings) Log() bool                               { return false }
func (plaintextSMTPSettings) Identifier() identifier.UserAgentUpdater { return nil }

func TestContainerSMTPRefusesPlaintextAuthentication(t *testing.T) {
	server := newSMTPServer(nil, plaintextSMTPSettings{})
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { defer close(done); _ = server.Serve(listener) }()
	defer func() { _ = server.Close(); <-done }()
	conn, err := net.DialTimeout("tcp", listener.Addr().String(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(time.Second))
	protocol := textproto.NewConn(conn)
	if _, _, err := protocol.ReadResponse(220); err != nil {
		t.Fatal(err)
	}
	if err := protocol.PrintfLine("EHLO test"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := protocol.ReadResponse(250); err != nil {
		t.Fatal(err)
	}
	if err := protocol.PrintfLine("AUTH PLAIN AHVzZXIAcGFzc3dvcmQ="); err != nil {
		t.Fatal(err)
	}
	code, _, err := protocol.ReadResponse(0)
	if err != nil || code != 523 {
		t.Fatalf("plaintext AUTH response=%d, error=%v", code, err)
	}
}
