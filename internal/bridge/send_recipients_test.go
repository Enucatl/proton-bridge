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

package bridge_test

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/ProtonMail/go-proton-api"
	"github.com/ProtonMail/go-proton-api/server"
	"github.com/ProtonMail/proton-bridge/v3/internal/bridge"
	bridgeMocks "github.com/ProtonMail/proton-bridge/v3/internal/bridge/mocks"
	"github.com/ProtonMail/proton-bridge/v3/internal/constants"
	"github.com/emersion/go-sasl"
	"github.com/emersion/go-smtp"
	"github.com/stretchr/testify/require"
)

func TestBridge_SendRejectsHeaderRecipientsOutsideEnvelope(t *testing.T) {
	withEnv(t, func(ctx context.Context, s *server.Server, netCtl *proton.NetCtl, locator bridge.Locator, storeKey []byte) {
		for _, name := range []string{"recipient", "header-only"} {
			_, _, err := s.CreateUser(name, password)
			require.NoError(t, err)
		}

		withBridge(ctx, t, s.GetHostURL(), netCtl, locator, storeKey, func(b *bridge.Bridge, _ *bridgeMocks.Mocks) {
			userID, err := b.LoginFull(ctx, username, password, nil, nil)
			require.NoError(t, err)
			info, err := b.GetUserInfo(userID)
			require.NoError(t, err)

			var mutations atomic.Int32
			s.AddCallWatcher(func(call server.Call) {
				if call.Method == http.MethodPost && call.URL.Path == "/mail/v4/messages" && call.RequestHeader.Get("X-HTTP-Method-Override") == http.MethodGet {
					return
				}
				if strings.HasPrefix(call.URL.Path, "/mail/v4/messages") && call.Method != http.MethodGet && call.Method != http.MethodHead {
					mutations.Add(1)
				}
			})

			client, err := dialSMTP(net.JoinHostPort(constants.Host, fmt.Sprint(b.GetSMTPPort())))
			require.NoError(t, err)
			defer client.Close() //nolint:errcheck
			require.NoError(t, client.Auth(sasl.NewPlainClient(info.Addresses[0], info.Addresses[0], string(info.BridgePass))))

			recipient := "recipient@" + s.GetDomain()
			omitted := "header-only@" + s.GetDomain()
			for _, header := range []string{"To", "Cc", "Bcc"} {
				t.Run(header, func(t *testing.T) {
					headers := fmt.Sprintf("%s: <%s>\r\n", header, omitted)
					if header != "To" {
						headers = "To: <" + recipient + ">\r\n" + headers
					}
					message := fmt.Sprintf("From: %s\r\n%sSubject: Rejected %s\r\n\r\nHello world!", info.Addresses[0], headers, header)
					// Retry To to verify recorder cleanup; keep the total within the
					// account's existing four-failure limit before cooldown.
					attempts := 1
					if header == "To" {
						attempts = 2
					}
					for range attempts {
						err := client.SendMail(info.Addresses[0], []string{recipient}, strings.NewReader(message))
						var smtpErr *smtp.SMTPError
						require.ErrorAs(t, err, &smtpErr)
						require.Equal(t, 554, smtpErr.Code)
						require.Equal(t, smtp.EnhancedCode{5, 6, 0}, smtpErr.EnhancedCode)
						require.Equal(t, "Cannot preserve message headers: Proton API requires every To, Cc and Bcc recipient to be included in RCPT TO", smtpErr.Message)
						require.Zero(t, mutations.Load(), "rejected submissions must not create, change, or send a draft")
					}
				})
			}

			for _, name := range []string{username, "recipient", "header-only"} {
				withClient(ctx, t, s, name, password, func(ctx context.Context, c *proton.Client) {
					messages, err := c.GetMessageMetadata(ctx, proton.MessageFilter{})
					require.NoError(t, err)
					require.Empty(t, messages, "rejected messages must not appear in Drafts, Sent, or recipients' Inbox")
				})
			}
		})
	})
}
