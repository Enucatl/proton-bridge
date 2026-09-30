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

//go:build container

package cli

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/ProtonMail/go-proton-api"
	"github.com/abiosoft/ishell"
)

type authenticatorInput struct {
	ishell.Actions
	reads int
}

func (input *authenticatorInput) ReadLine() string {
	input.reads++
	return "123456"
}

func (*authenticatorInput) Printf(string, ...interface{}) {}
func (*authenticatorInput) Println(...interface{})        {}

func TestContainerTwoFactor(t *testing.T) {
	for _, status := range []proton.TwoFAStatus{0, proton.HasTOTP, proton.HasFIDO2, proton.HasFIDO2AndTOTP} {
		t.Run(string(rune('0'+status)), func(t *testing.T) {
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				var request proton.Auth2FAReq
				if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
					t.Error(err)
				}
				if r.URL.Path != "/auth/v4/2fa" || request.TwoFactorCode != "123456" {
					t.Error("expected authenticator code authentication")
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, `{"Code":1000}`)
			}))
			defer server.Close()
			manager := proton.New(proton.WithHostURL(server.URL))
			defer manager.Close()
			input := &authenticatorInput{}
			frontend := &frontendCLI{Shell: &ishell.Shell{Actions: input}}
			context := &ishell.Context{Actions: input}
			auth := proton.Auth{TwoFA: proton.TwoFAInfo{Enabled: status}}
			if got := frontend.loginTwoFactor(context, manager.NewClient("uid", "access", "refresh"), auth); got != (status != proton.HasFIDO2) {
				t.Errorf("unexpected authentication result: %v", got)
			}
			want := 0
			if status == proton.HasTOTP || status == proton.HasFIDO2AndTOTP {
				want = 1
			}
			if int(requests.Load()) != want || input.reads != want {
				t.Errorf("requests=%d input reads=%d; want %d", requests.Load(), input.reads, want)
			}
		})
	}
}
