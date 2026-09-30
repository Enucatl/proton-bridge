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

package sentry

import (
	"testing"

	sentrysdk "github.com/getsentry/sentry-go"
)

func TestContainerDoesNotSendCrashReports(t *testing.T) {
	previous := skippedFunctions
	defer func() { skippedFunctions = previous }()
	// A zero-value reporter has no identifier: reaching upload preparation would panic.
	reporter := &Reporter{}
	if err := reporter.scopedReport(nil, func(_ *sentrysdk.Scope) {
		t.Fatal("container invoked the crash upload callback")
	}); err != nil {
		t.Fatal(err)
	}
}
