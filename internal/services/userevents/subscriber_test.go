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

package userevents

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"
)

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}

func TestChanneledSubscriber_CtxTimeoutDoesNotBlockFutureEvents(t *testing.T) {
	var wg sync.WaitGroup
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	subscriber := newChanneledSubscriber[int]("test")
	defer func() {
		cancel()
		wg.Wait()
		subscriber.close()
	}()
	results := make(chan error, 2)

	wg.Go(func() {
		results <- subscriber.handle(ctx, 30)

		// An expired deadline fails immediately without a receiver.
		expired, cancel := context.WithDeadline(ctx, time.Unix(0, 0))
		defer cancel()
		results <- subscriber.handle(expired, 20)
	})

	// Receive first event. Notify success.
	event, ok := <-subscriber.OnEventCh()
	require.True(t, ok)
	event.Consume(func(event int) error {
		require.Equal(t, 30, event)
		return nil
	})
	require.NoError(t, <-results)
	require.ErrorIs(t, <-results, context.DeadlineExceeded)

	// Simulate reception of another event
	wg.Go(func() {
		results <- subscriber.handle(ctx, 40)
	})

	event, ok = <-subscriber.OnEventCh()
	require.True(t, ok)
	event.Consume(func(event int) error {
		require.Equal(t, 40, event)
		return nil
	})

	require.NoError(t, <-results)
}

func TestChanneledSubscriber_ErrorReported(t *testing.T) {
	var wg sync.WaitGroup
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	subscriber := newChanneledSubscriber[int]("test")
	defer func() {
		cancel()
		wg.Wait()
		subscriber.close()
	}()
	results := make(chan error, 2)
	reportedErr := fmt.Errorf("request failed")

	wg.Go(func() {
		results <- subscriber.handle(ctx, 30)
	})

	// Receive first event. Notify success.
	event, ok := <-subscriber.OnEventCh()
	require.True(t, ok)
	event.Consume(func(event int) error {
		require.Equal(t, 30, event)
		return reportedErr
	})

	require.ErrorIs(t, <-results, reportedErr)
}
