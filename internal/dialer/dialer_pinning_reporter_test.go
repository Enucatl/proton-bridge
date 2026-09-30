// Copyright (c) 2026 Proton AG
//
// This file is part of Proton Mail Bridge.Bridge.
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

package dialer

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/ProtonMail/proton-bridge/v3/internal/constants"
	"github.com/ProtonMail/proton-bridge/v3/internal/useragent"
	"github.com/stretchr/testify/assert"
)

func TestTLSReporter_DoubleReport(t *testing.T) {
	var reportCounter atomic.Int64

	reportServer := httptest.NewTLSServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		reportCounter.Add(1)
	}))
	defer reportServer.Close()

	r := NewTLSReporter("hostURL", "appVersion", useragent.New(), TrustedAPIPins)

	// Report the same issue many times.
	for range 10 {
		r.ReportCertIssue(reportServer.URL, "myhost", "443", tls.ConnectionState{})
	}

	// Duplicate reports are recorded once; containers do not upload them.
	assert.Len(t, r.sentReports, 1)
	var wantUploads int64
	if !constants.IsContainer {
		wantUploads = 1
	}
	assert.Equal(t, wantUploads, reportCounter.Load())

	// If we then report something else many times.
	for range 10 {
		r.ReportCertIssue(reportServer.URL, "anotherhost", "443", tls.ConnectionState{})
	}

	assert.Len(t, r.sentReports, 2)
	assert.Equal(t, wantUploads*2, reportCounter.Load())
}
