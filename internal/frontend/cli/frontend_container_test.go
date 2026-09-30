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
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU General Public License for more details.
//
// You should have received a copy of the GNU General Public License
// along with Proton Mail Bridge. If not, see <https://www.gnu.org/licenses/>.

//go:build container

package cli

import (
	"io"
	"strings"
	"testing"

	"github.com/ProtonMail/gluon/async"
	"github.com/ProtonMail/proton-bridge/v3/internal/bridge"
	"github.com/ProtonMail/proton-bridge/v3/internal/events"
	"github.com/abiosoft/ishell"
	"github.com/abiosoft/readline"
)

func TestContainerCommands(t *testing.T) {
	eventCh := make(chan events.Event)
	close(eventCh)
	quitCh := make(chan struct{})
	stdin := readline.Stdin
	readline.Stdin = io.NopCloser(strings.NewReader(""))
	frontend := New(&bridge.Bridge{}, nil, eventCh, async.NoopPanicHandler{}, quitCh)
	readline.Stdin = stdin
	defer close(quitCh)
	// Read EOF to let readline's terminal worker start before Close waits for it.
	if _, err := frontend.ReadLineErr(); err != io.EOF {
		t.Fatalf("read empty input: %v", err)
	}

	root := &ishell.Cmd{}
	for _, command := range frontend.Cmds() {
		root.AddCmd(command)
	}
	for _, path := range [][]string{
		{"clear", "accounts"}, {"clear", "everything"}, {"updates"}, {"telemetry"},
		{"change", "imap-port"}, {"change", "smtp-port"},
		{"change", "imap-security"}, {"change", "smtp-security"},
		{"cert", "import"}, {"configure-apple-mail"},
	} {
		if command, remaining := root.FindCmd(path); command != nil && len(remaining) == 0 {
			t.Errorf("desktop command available in container: %v", path)
		}
	}
	for _, path := range [][]string{
		{"login"}, {"list"}, {"change", "mode"},
		{"change", "change-location"}, {"cert", "export"},
	} {
		if command, remaining := root.FindCmd(path); command == nil || len(remaining) != 0 {
			t.Errorf("container command missing: %v", path)
		}
	}
}
