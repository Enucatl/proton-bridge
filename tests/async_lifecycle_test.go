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
	"testing"

	"github.com/ProtonMail/proton-bridge/v3/internal/events"
	"github.com/emersion/go-imap"
	"github.com/emersion/go-imap/client"
	"go.uber.org/goleak"
)

func TestEventCollector_CloseStopsCollection(t *testing.T) {
	defer goleak.VerifyNone(t, goleak.IgnoreCurrent())

	collector := newEventCollector()
	forwarded := collector.collectFrom(make(chan events.Event))
	queued := collector.getEventCh(events.AllUsersLoaded{})
	collector.push(events.AllUsersLoaded{})
	collector.close()

	if _, ok := <-forwarded; ok {
		t.Fatal("forwarded event channel is still open")
	}
	if _, ok := <-queued; ok {
		t.Fatal("queued event channel is still open")
	}
}

func TestIMAPHelpers_ReturnCommandErrors(t *testing.T) {
	defer goleak.VerifyNone(t, goleak.IgnoreCurrent())

	unauthenticated := new(client.Client)
	if _, err := clientList(unauthenticated); err == nil {
		t.Fatal("LIST succeeded without authentication")
	}
	for _, uid := range []bool{false, true} {
		if _, err := clientFetchSequence(unauthenticated, "1", uid); err == nil {
			t.Fatal("FETCH succeeded without selecting a mailbox")
		}
		if _, err := clientStore(unauthenticated, 1, 1, uid, imap.AddFlags, imap.SeenFlag); err == nil {
			t.Fatal("STORE succeeded without selecting a mailbox")
		}
	}
}
