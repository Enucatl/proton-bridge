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

package imapsmtpserver

import (
	"crypto/tls"
	"time"

	"github.com/ProtonMail/proton-bridge/v3/internal/constants"
	"github.com/ProtonMail/proton-bridge/v3/internal/identifier"
	"github.com/ProtonMail/proton-bridge/v3/internal/logging"
	smtpservice "github.com/ProtonMail/proton-bridge/v3/internal/services/smtp"
	"github.com/emersion/go-sasl"
	"github.com/emersion/go-smtp"
	"github.com/sirupsen/logrus"
)

var logSMTP = logrus.WithField("pkg", "server/smtp") //nolint:gochecknoglobals

type SMTPSettingsProvider interface {
	TLSConfig() *tls.Config
	Log() bool
	Port() int
	SetPort(int) error
	UseSSL() bool
	Identifier() identifier.UserAgentUpdater
}

func newSMTPServer(accounts *smtpservice.Accounts, settings SMTPSettingsProvider) *smtp.Server {
	logSMTP.WithField("logSMTP", settings.Log()).Info("Creating SMTP server")

	smtpServer := smtp.NewServer(smtpservice.NewBackend(accounts, settings.Identifier()))

	smtpServer.TLSConfig = settings.TLSConfig()
	smtpServer.Domain = constants.Host
	smtpServer.AllowInsecureAuth = !constants.IsContainer
	smtpServer.MaxLineLength = 1 << 16
	// Bound submissions and stalled connections; the size cap includes MIME encoding headroom.
	// See https://github.com/Enucatl/proton-bridge/issues/5 for Proton's limits and this policy.
	smtpServer.MaxRecipients = 100
	smtpServer.MaxMessageBytes = 100 << 20
	smtpServer.ReadTimeout = 5 * time.Minute
	smtpServer.WriteTimeout = 5 * time.Minute
	smtpServer.ErrorLog = logging.NewSMTPLogger()

	smtpServer.EnableAuth("LOGIN", func(conn *smtp.Conn) sasl.Server {
		return &loginServer{session: conn.Session()}
	})

	if settings.Log() {
		logSMTP.Warning("================================================")
		logSMTP.Warning("THIS LOG WILL CONTAIN **DECRYPTED** MESSAGE DATA")
		logSMTP.Warning("================================================")

		smtpServer.Debug = logging.NewSMTPDebugLogger()
	}

	return smtpServer
}

// loginServer preserves legacy LOGIN support after go-sasl dropped its server implementation.
// See https://github.com/emersion/go-sasl/issues/19.
type loginServer struct {
	step     int
	username string
	session  smtp.Session
}

func (s *loginServer) Next(response []byte) ([]byte, bool, error) {
	switch s.step {
	case 0:
		s.step = 1
		if response == nil {
			return []byte("Username:"), false, nil
		}
		fallthrough
	case 1:
		s.username = string(response)
		s.step = 2
		return []byte("Password:"), false, nil
	case 2:
		s.step = 3
		return nil, true, s.session.AuthPlain(s.username, string(response))
	default:
		return nil, false, sasl.ErrUnexpectedClientResponse
	}
}
