//go:build container

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

package bridge

import (
	"runtime/debug"
	"strings"
)

// Credits describes the modules actually compiled into the container variant.
var Credits = func() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "Proton Mail Bridge; see distributed third-party license notices"
	}
	names := make([]string, 0, len(info.Deps))
	for _, dep := range info.Deps {
		if dep.Replace != nil {
			dep = dep.Replace
		}
		names = append(names, dep.Path+" "+dep.Version)
	}
	return strings.Join(names, ";")
}()
