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
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/ProtonMail/proton-bridge/v3/internal/identifier"
	"github.com/ProtonMail/proton-bridge/v3/internal/useragent"
	"github.com/emersion/go-smtp"
	"github.com/sirupsen/logrus"
)

type Backend struct {
	accounts  *Accounts
	userAgent identifier.UserAgentUpdater
}

func NewBackend(accounts *Accounts, userAgent identifier.UserAgentUpdater) *Backend {
	return &Backend{
		accounts:  accounts,
		userAgent: userAgent,
	}
}

type smtpSession struct {
	accounts  *Accounts
	userAgent identifier.UserAgentUpdater

	userID string
	authID string

	from string
	to   []string
}

func (be *Backend) NewSession(*smtp.Conn) (smtp.Session, error) {
	return &smtpSession{accounts: be.accounts, userAgent: be.userAgent}, nil
}

func (s *smtpSession) AuthPlain(username, password string) error {
	userID, authID, err := s.accounts.CheckAuth(username, []byte(password))
	if err != nil {
		if !errors.Is(err, ErrNoSuchUser) {
			return fmt.Errorf("unknown error")
		}
		logrus.WithFields(logrus.Fields{
			"username": username,
			"pkg":      "smtp",
		}).Error("Incorrect login credentials.")

		return fmt.Errorf("invalid username or password")
	}

	s.userID = userID
	s.authID = authID

	if strings.Contains(s.userAgent.GetUserAgent(), useragent.DefaultUserAgent) {
		s.userAgent.SetUserAgent(useragent.UnknownClient, useragent.DefaultVersion)
	}

	return nil
}

func (s *smtpSession) Reset() {
	s.from = ""
	s.to = nil
}

func (s *smtpSession) Logout() error {
	s.Reset()
	s.userID = ""
	s.authID = ""
	return nil
}

// Require authentication before accepting envelope or message data.
// See https://github.com/Enucatl/proton-bridge/issues/5.
func (s *smtpSession) requireAuth() error {
	if s.userID == "" {
		return &smtp.SMTPError{
			Code:         530,
			EnhancedCode: smtp.EnhancedCode{5, 7, 0},
			Message:      "Authentication required",
		}
	}
	return nil
}

func (s *smtpSession) Mail(from string, _ *smtp.MailOptions) error {
	if err := s.requireAuth(); err != nil {
		return err
	}
	s.from = from
	return nil
}

func (s *smtpSession) Rcpt(to string) error {
	if err := s.requireAuth(); err != nil {
		return err
	}
	if len(to) > 0 {
		s.to = append(s.to, to)
	}

	return nil
}

func (s *smtpSession) Data(r io.Reader) error {
	if err := s.requireAuth(); err != nil {
		return err
	}
	err := s.accounts.SendMail(context.Background(), s.userID, s.authID, s.from, s.to, r)
	if err != nil {
		logrus.WithFields(logrus.Fields{
			"pkg":  "smtp",
			"user": s.userID,
		}).WithError(err).Error("Send mail failed.")
		// Preserve the size-limit response through the submission error wrappers (issue #5).
		if errors.Is(err, smtp.ErrDataTooLarge) {
			return smtp.ErrDataTooLarge
		}
		return mapError(err)
	}
	return nil
}
