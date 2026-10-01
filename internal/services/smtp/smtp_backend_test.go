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

package smtp

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ProtonMail/proton-bridge/v3/pkg/cpc"
	"github.com/emersion/go-smtp"
	"github.com/stretchr/testify/require"
)

func TestSMTPSessionRequiresAuthentication(t *testing.T) {
	session := smtpSession{from: "existing@example.com", to: []string{"existing-recipient@example.com"}}
	reader := strings.NewReader("message body")
	for _, err := range []error{
		session.Mail("sender@example.com", nil),
		session.Rcpt("recipient@example.com"),
		session.Data(reader),
	} {
		require.Equal(t, &smtp.SMTPError{
			Code:         530,
			EnhancedCode: smtp.EnhancedCode{5, 7, 0},
			Message:      "Authentication required",
		}, err)
	}
	require.Equal(t, "existing@example.com", session.from)
	require.Equal(t, []string{"existing-recipient@example.com"}, session.to)
	require.Equal(t, len("message body"), reader.Len())
}

func TestSMTPSessionAuthenticationLifecycle(t *testing.T) {
	session := smtpSession{userID: "user", authID: "address"}
	require.NoError(t, session.Mail("sender@example.com", nil))
	require.NoError(t, session.Rcpt("recipient@example.com"))
	require.Equal(t, "sender@example.com", session.from)
	require.Equal(t, []string{"recipient@example.com"}, session.to)

	session.Reset()
	require.Empty(t, session.from)
	require.Empty(t, session.to)
	require.Equal(t, "user", session.userID)
	require.Equal(t, "address", session.authID)
	require.NoError(t, session.Mail("sender@example.com", nil))

	require.NoError(t, session.Logout())
	require.Empty(t, session.from)
	require.Empty(t, session.to)
	require.Empty(t, session.userID)
	require.Empty(t, session.authID)
	require.Error(t, session.Mail("sender@example.com", nil))
}

func TestSMTPSessionPreservesMessageSizeError(t *testing.T) {
	service := &Service{userID: "user", cpc: cpc.NewCPC()}
	accounts := NewAccounts()
	accounts.AddAccount(service)
	session := smtpSession{accounts: accounts, userID: "user", authID: "address", to: []string{"recipient@example.com"}}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	go func() {
		select {
		case request := <-service.cpc.ReceiveCh():
			request.Reply(ctx, nil, fmt.Errorf("failed to read message: %w", smtp.ErrDataTooLarge))
		case <-ctx.Done():
		}
	}()

	result := make(chan error, 1)
	go func() { result <- session.Data(strings.NewReader("message body")) }()
	select {
	case err := <-result:
		require.Same(t, smtp.ErrDataTooLarge, err)
	case <-ctx.Done():
		t.Fatal("SMTP submission did not return its size error")
	}
}
