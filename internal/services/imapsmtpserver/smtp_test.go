// Copyright (c) 2026 Proton AG
//
// This file is part of Proton Mail Bridge.
//
// Proton Mail Bridge is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.

package imapsmtpserver

import (
	"crypto/tls"
	"errors"
	"io"
	"net"
	"net/http/httptest"
	"net/textproto"
	"strings"
	"testing"
	"time"

	"github.com/ProtonMail/proton-bridge/v3/internal/identifier"
	"github.com/emersion/go-smtp"
	"github.com/stretchr/testify/require"
)

type testSMTPSettings struct{ SMTPSettingsProvider }

func (testSMTPSettings) TLSConfig() *tls.Config                  { return nil }
func (testSMTPSettings) Log() bool                               { return false }
func (testSMTPSettings) Identifier() identifier.UserAgentUpdater { return nil }

func serveSMTP(t *testing.T, server *smtp.Server, listener net.Listener) {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- server.Serve(listener) }()
	t.Cleanup(func() {
		require.NoError(t, server.Close())
		require.NoError(t, <-done)
	})
}

func dialSMTP(t *testing.T, server *smtp.Server) *textproto.Conn {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	serveSMTP(t, server, listener)
	conn, err := net.DialTimeout("tcp", listener.Addr().String(), 5*time.Second)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	require.NoError(t, conn.SetDeadline(time.Now().Add(5*time.Second)))
	protocol := textproto.NewConn(conn)
	_, _, err = protocol.ReadResponse(220)
	require.NoError(t, err)
	return protocol
}

func smtpCommand(t *testing.T, protocol *textproto.Conn, command string, want int) string {
	t.Helper()
	require.NoError(t, protocol.PrintfLine("%s", command))
	_, message, err := protocol.ReadResponse(want)
	require.NoError(t, err, command)
	return message
}

func TestSMTPRejectsUnauthenticatedSubmission(t *testing.T) {
	protocol := dialSMTP(t, newSMTPServer(nil, testSMTPSettings{}))
	smtpCommand(t, protocol, "EHLO test", 250)
	for _, command := range []string{"MAIL FROM:<sender@example.com>", "RCPT TO:<recipient@example.com>", "DATA", "BDAT 0 LAST"} {
		want := 502 // go-smtp rejects RCPT/DATA/BDAT without a successful MAIL.
		if strings.HasPrefix(command, "MAIL") {
			want = 530
		}
		message := smtpCommand(t, protocol, command, want)
		if want == 530 {
			require.Equal(t, "5.7.0 Authentication required", message)
		}
	}
	smtpCommand(t, protocol, "NOOP", 250)
	smtpCommand(t, protocol, "RSET", 250)
	smtpCommand(t, protocol, "QUIT", 221)
}

// Use a consuming backend to test the configured transport limits without a Proton account.
type consumingSMTPBackend struct{}

func (consumingSMTPBackend) NewSession(*smtp.Conn) (smtp.Session, error) {
	return consumingSMTPBackend{}, nil
}
func (consumingSMTPBackend) AuthPlain(string, string) error       { return nil }
func (consumingSMTPBackend) Mail(string, *smtp.MailOptions) error { return nil }
func (consumingSMTPBackend) Rcpt(string) error                    { return nil }
func (consumingSMTPBackend) Reset()                               {}
func (consumingSMTPBackend) Logout() error                        { return nil }
func (consumingSMTPBackend) Data(r io.Reader) error {
	_, err := io.Copy(io.Discard, r)
	return err
}

func TestSMTPSubmissionLimits(t *testing.T) {
	server := newSMTPServer(nil, testSMTPSettings{})
	require.Equal(t, 100, server.MaxRecipients)
	require.Equal(t, 100<<20, server.MaxMessageBytes)
	server.Backend = consumingSMTPBackend{}
	server.AllowInsecureAuth = true
	// Exercise actual DATA byte enforcement with a small payload.
	server.MaxMessageBytes = 64
	protocol := dialSMTP(t, server)
	capabilities := smtpCommand(t, protocol, "EHLO test", 250)
	require.Contains(t, capabilities, "SIZE 64")
	require.Regexp(t, `(?m)^AUTH (PLAIN LOGIN|LOGIN PLAIN)$`, capabilities)
	smtpCommand(t, protocol, "AUTH PLAIN AHVzZXIAcGFzc3dvcmQ=", 235)
	smtpCommand(t, protocol, "MAIL FROM:<sender@example.com> SIZE=65", 552)
	smtpCommand(t, protocol, "MAIL FROM:<sender@example.com>", 250)
	for i := 0; i < 100; i++ {
		smtpCommand(t, protocol, "RCPT TO:<recipient@example.com>", 250)
	}
	smtpCommand(t, protocol, "RCPT TO:<extra@example.com>", 552)
	smtpCommand(t, protocol, "DATA", 354)
	require.NoError(t, protocol.PrintfLine("%s\r\n.", strings.Repeat("x", 65)))
	_, message, err := protocol.ReadResponse(552)
	require.NoError(t, err)
	require.Contains(t, message, "5.3.4")
	smtpCommand(t, protocol, "MAIL FROM:<sender@example.com>", 250)
	smtpCommand(t, protocol, "RCPT TO:<recipient@example.com>", 250)
	// BDAT must enforce the limit even without a declared MAIL SIZE.
	require.NoError(t, protocol.PrintfLine("BDAT 65 LAST"))
	require.NoError(t, protocol.PrintfLine("%s", strings.Repeat("x", 63)))
	_, _, err = protocol.ReadResponse(552)
	require.NoError(t, err)
	smtpCommand(t, protocol, "QUIT", 221)
}

type loginSMTPBackend struct{ consumingSMTPBackend }

func (loginSMTPBackend) NewSession(*smtp.Conn) (smtp.Session, error) {
	return loginSMTPBackend{}, nil
}

func (loginSMTPBackend) AuthPlain(username, password string) error {
	if username != "user" || password != "password" {
		return errors.New("invalid username or password")
	}
	return nil
}

func TestSMTPLogin(t *testing.T) {
	for _, test := range []struct {
		name     string
		initial  bool
		password string
		want     int
	}{
		{"challenges", false, "cGFzc3dvcmQ=", 235},
		{"initial response", true, "cGFzc3dvcmQ=", 235},
		{"challenges with invalid password", false, "d3Jvbmc=", 454},
		{"initial response with invalid password", true, "d3Jvbmc=", 454},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := newSMTPServer(nil, testSMTPSettings{})
			server.Backend = loginSMTPBackend{}
			server.AllowInsecureAuth = true
			protocol := dialSMTP(t, server)
			smtpCommand(t, protocol, "EHLO test", 250)
			if test.initial {
				require.Equal(t, "UGFzc3dvcmQ6", smtpCommand(t, protocol, "AUTH LOGIN dXNlcg==", 334))
			} else {
				require.Equal(t, "VXNlcm5hbWU6", smtpCommand(t, protocol, "AUTH LOGIN", 334))
				require.Equal(t, "UGFzc3dvcmQ6", smtpCommand(t, protocol, "dXNlcg==", 334))
			}
			smtpCommand(t, protocol, test.password, test.want)
			smtpCommand(t, protocol, "QUIT", 221)
		})
	}
}

func TestSMTPHandshakeTimeout(t *testing.T) {
	fixture := httptest.NewTLSServer(nil)
	config := fixture.TLS.Clone()
	fixture.Close()
	server := newSMTPServer(nil, testSMTPSettings{})
	require.Equal(t, 5*time.Minute, server.ReadTimeout)
	require.Equal(t, 5*time.Minute, server.WriteTimeout)
	server.ReadTimeout = 100 * time.Millisecond
	listener, err := tls.Listen("tcp", "127.0.0.1:0", config)
	require.NoError(t, err)
	serveSMTP(t, server, listener)
	conn, err := net.DialTimeout("tcp", listener.Addr().String(), time.Second)
	require.NoError(t, err)
	defer conn.Close()
	require.NoError(t, conn.SetReadDeadline(time.Now().Add(5*time.Second)))
	_, err = conn.Read(make([]byte, 1))
	require.ErrorIs(t, err, io.EOF)
}
