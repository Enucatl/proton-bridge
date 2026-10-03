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
	"io"
	"testing"
	"time"

	"github.com/ProtonMail/gluon/async"
	"github.com/ProtonMail/go-proton-api"
	"github.com/ProtonMail/proton-bridge/v3/internal/events"
	mocks2 "github.com/ProtonMail/proton-bridge/v3/internal/events/mocks"
	"github.com/ProtonMail/proton-bridge/v3/internal/sentry"
	"github.com/ProtonMail/proton-bridge/v3/internal/services/orderedtasks"
	"github.com/ProtonMail/proton-bridge/v3/internal/services/userevents/mocks"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func TestService_EventIDLoadStore(t *testing.T) {
	// Simulate the flow of data when we start without any event id in the event id store:
	// * Load event id from store
	// * Get latest event id since
	// * Store that latest event id
	// * Start event poll loop
	// * Get new event id, store it in vault
	// * Try to poll new event it, but context is cancelled
	group := orderedtasks.NewOrderedCancelGroup(async.NoopPanicHandler{})
	mockCtrl := gomock.NewController(t)
	eventPublisher := mocks2.NewMockEventPublisher(mockCtrl)
	eventIDStore := mocks.NewMockEventIDStore(mockCtrl)
	eventSource := mocks.NewMockEventSource(mockCtrl)

	firstEventID := "EVENT01"
	secondEventID := "EVENT02"
	secondEvent := []proton.Event{{
		EventID: secondEventID,
	}}

	// Event id store expectations.
	eventIDStore.EXPECT().Load(gomock.Any()).Times(1).Return("", nil)
	eventIDStore.EXPECT().Store(gomock.Any(), gomock.Eq(firstEventID)).Times(1).Return(nil)
	eventIDStore.EXPECT().Store(gomock.Any(), gomock.Eq(secondEventID)).Times(1).DoAndReturn(func(_ context.Context, _ string) error {
		// Force exit, we have finished executing what we expected.
		group.Cancel()
		return nil
	})

	// Event Source expectations.
	eventSource.EXPECT().GetLatestEventID(gomock.Any()).Times(1).Return(firstEventID, nil)
	eventSource.EXPECT().GetEvent(gomock.Any(), gomock.Eq(firstEventID)).MinTimes(1).Return(secondEvent, false, nil)

	service := NewService(
		"foo",
		eventSource,
		eventIDStore,
		eventPublisher,
		time.Millisecond,
		time.Millisecond,
		time.Second,
		async.NoopPanicHandler{},
		events.NewNullSubscription(),
		sentry.NullSentryReporter{},
	)
	defer func() { group.CancelAndWait(); service.Close() }()

	_, err := service.Start(context.Background(), group)
	require.NoError(t, err)

	service.Resume()
	group.Wait()
}

func TestService_RetryEventOnNonCatastrophicFailure(t *testing.T) {
	group := orderedtasks.NewOrderedCancelGroup(async.NoopPanicHandler{})
	mockCtrl := gomock.NewController(t)
	eventPublisher := mocks2.NewMockEventPublisher(mockCtrl)
	eventIDStore := mocks.NewMockEventIDStore(mockCtrl)
	eventSource := mocks.NewMockEventSource(mockCtrl)
	subscriber := NewMockMessageEventHandler(mockCtrl)

	firstEventID := "EVENT01"
	secondEventID := "EVENT02"
	messageEvents := []proton.MessageEvent{
		{
			EventItem: proton.EventItem{ID: "Message"},
		},
	}
	secondEvent := []proton.Event{{
		EventID:  secondEventID,
		Messages: messageEvents,
	}}

	// Event id store expectations.
	eventIDStore.EXPECT().Load(gomock.Any()).Times(1).Return(firstEventID, nil)
	eventIDStore.EXPECT().Store(gomock.Any(), gomock.Eq(secondEventID)).Times(1).DoAndReturn(func(_ context.Context, _ string) error {
		// Force exit, we have finished executing what we expected.
		group.Cancel()
		return nil
	})

	// Event Source expectations.
	eventSource.EXPECT().GetEvent(gomock.Any(), gomock.Eq(firstEventID)).MinTimes(1).Return(secondEvent, false, nil)

	// Subscriber expectations.
	{
		firstCall := subscriber.EXPECT().HandleMessageEvents(gomock.Any(), gomock.Eq(messageEvents)).Times(1).Return(io.ErrUnexpectedEOF)
		subscriber.EXPECT().HandleMessageEvents(gomock.Any(), gomock.Eq(messageEvents)).After(firstCall).Times(1).Return(nil)
	}

	service := NewService(
		"foo",
		eventSource,
		eventIDStore,
		eventPublisher,
		time.Millisecond,
		time.Millisecond,
		time.Second,
		async.NoopPanicHandler{},
		events.NewNullSubscription(),
		sentry.NullSentryReporter{},
	)
	defer func() { group.CancelAndWait(); service.Close() }()
	service.Subscribe(NewCallbackSubscriber("foo", EventHandler{MessageHandler: subscriber}))

	_, err := service.Start(context.Background(), group)
	require.NoError(t, err)

	service.Resume()
	group.Wait()
}

func TestService_OnBadEventServiceIsPaused(t *testing.T) {
	group := orderedtasks.NewOrderedCancelGroup(async.NoopPanicHandler{})
	mockCtrl := gomock.NewController(t)
	eventPublisher := mocks2.NewMockEventPublisher(mockCtrl)
	eventIDStore := mocks.NewMockEventIDStore(mockCtrl)
	eventSource := mocks.NewMockEventSource(mockCtrl)
	subscriber := NewMockMessageEventHandler(mockCtrl)

	firstEventID := "EVENT01"
	secondEventID := "EVENT02"
	messageEvents := []proton.MessageEvent{
		{
			EventItem: proton.EventItem{ID: "Message"},
		},
	}
	secondEvent := []proton.Event{{
		EventID:  secondEventID,
		Messages: messageEvents,
	}}

	// Event id store expectations.
	eventIDStore.EXPECT().Load(gomock.Any()).Times(1).Return(firstEventID, nil)

	// Event Source expectations.
	eventSource.EXPECT().GetEvent(gomock.Any(), gomock.Eq(firstEventID)).MinTimes(1).Return(secondEvent, false, nil)

	// Subscriber expectations.
	badEventErr := fmt.Errorf("I will cause bad event")
	subscriber.EXPECT().HandleMessageEvents(gomock.Any(), gomock.Eq(messageEvents)).Times(1).Return(badEventErr)

	service := NewService(
		"foo",
		eventSource,
		eventIDStore,
		eventPublisher,
		time.Millisecond,
		time.Millisecond,
		time.Second,
		async.NoopPanicHandler{},
		events.NewNullSubscription(),
		sentry.NullSentryReporter{},
	)
	defer func() { group.CancelAndWait(); service.Close() }()

	// Event publisher expectations.
	badEvent := make(chan struct{})
	eventPublisher.EXPECT().PublishEvent(gomock.Any(), events.UserBadEvent{
		UserID:     "foo",
		OldEventID: firstEventID,
		NewEventID: secondEventID,
		EventInfo:  secondEvent[0].String(),
		Error:      fmt.Errorf("failed to apply message events: %w", badEventErr),
	}).Do(func(_ context.Context, _ events.Event) {
		close(badEvent)
	})

	service.Subscribe(NewCallbackSubscriber("foo", EventHandler{MessageHandler: subscriber}))

	_, err := service.Start(context.Background(), group)
	require.NoError(t, err)

	service.Resume()
	<-badEvent
	require.True(t, service.IsPaused())
	group.CancelAndWait()
}

func TestService_UnsubscribeDuringEventHandlingDoesNotCauseDeadlock(t *testing.T) {
	group := orderedtasks.NewOrderedCancelGroup(async.NoopPanicHandler{})
	mockCtrl := gomock.NewController(t)
	eventPublisher := mocks2.NewMockEventPublisher(mockCtrl)
	eventIDStore := mocks.NewMockEventIDStore(mockCtrl)
	eventSource := mocks.NewMockEventSource(mockCtrl)
	subscriber := NewMockMessageEventHandler(mockCtrl)

	firstEventID := "EVENT01"
	secondEventID := "EVENT02"
	messageEvents := []proton.MessageEvent{
		{
			EventItem: proton.EventItem{ID: "Message"},
		},
	}
	secondEvent := []proton.Event{{
		EventID:  secondEventID,
		Messages: messageEvents,
	}}

	// Event id store expectations.
	eventIDStore.EXPECT().Load(gomock.Any()).Times(1).Return(firstEventID, nil)
	eventIDStore.EXPECT().Store(gomock.Any(), gomock.Eq(secondEventID)).Times(1).DoAndReturn(func(_ context.Context, _ string) error {
		// Force exit, we have finished executing what we expected.
		group.Cancel()
		return nil
	})

	// Event Source expectations.
	eventSource.EXPECT().GetEvent(gomock.Any(), gomock.Eq(firstEventID)).MinTimes(1).Return(secondEvent, false, nil)

	service := NewService(
		"foo",
		eventSource,
		eventIDStore,
		eventPublisher,
		time.Millisecond,
		time.Millisecond,
		time.Second,
		async.NoopPanicHandler{},
		events.NewNullSubscription(),
		sentry.NullSentryReporter{},
	)
	defer func() { group.CancelAndWait(); service.Close() }()

	subscription := NewCallbackSubscriber("foo", EventHandler{MessageHandler: subscriber})

	// Subscriber expectations.
	subscriber.EXPECT().HandleMessageEvents(gomock.Any(), gomock.Eq(messageEvents)).Times(1).DoAndReturn(func(_ context.Context, _ []proton.MessageEvent) error {
		service.Unsubscribe(subscription)
		return nil
	})

	service.Subscribe(subscription)

	_, err := service.Start(context.Background(), group)
	require.NoError(t, err)

	service.Resume()
	group.Wait()
}

func TestService_UnsubscribeBeforeHandlingEventIsNotConsideredError(t *testing.T) {
	group := orderedtasks.NewOrderedCancelGroup(async.NoopPanicHandler{})
	mockCtrl := gomock.NewController(t)
	eventPublisher := mocks2.NewMockEventPublisher(mockCtrl)
	eventIDStore := mocks.NewMockEventIDStore(mockCtrl)
	eventSource := mocks.NewMockEventSource(mockCtrl)

	firstEventID := "EVENT01"
	secondEventID := "EVENT02"
	messageEvents := []proton.MessageEvent{
		{
			EventItem: proton.EventItem{ID: "Message"},
		},
	}
	secondEvent := []proton.Event{{
		EventID:  secondEventID,
		Messages: messageEvents,
	}}

	// Event id store expectations.
	eventIDStore.EXPECT().Load(gomock.Any()).Times(1).Return(firstEventID, nil)
	eventIDStore.EXPECT().Store(gomock.Any(), gomock.Eq(secondEventID)).Times(1).DoAndReturn(func(_ context.Context, _ string) error {
		// Force exit, we have finished executing what we expected.
		group.Cancel()
		return nil
	})

	// Event Source expectations.
	eventFetched := make(chan struct{})
	eventSource.EXPECT().GetEvent(gomock.Any(), gomock.Eq(firstEventID)).Times(1).DoAndReturn(func(_ context.Context, _ string) ([]proton.Event, bool, error) {
		close(eventFetched)
		return secondEvent, false, nil
	})
	eventSource.EXPECT().GetEvent(gomock.Any(), gomock.Eq(secondEventID)).AnyTimes().Return(secondEvent, false, nil)

	service := NewService(
		"foo",
		eventSource,
		eventIDStore,
		eventPublisher,
		time.Millisecond,
		time.Millisecond,
		time.Second,
		async.NoopPanicHandler{},
		events.NewNullSubscription(),
		sentry.NullSentryReporter{},
	)
	defer func() { group.CancelAndWait(); service.Close() }()

	subscription := NewEventSubscriber("Foo")

	// Unsubscribe once the service fetches the event, without receiving it.
	group.Go(context.Background(), "", "", func(ctx context.Context) {
		defer service.Unsubscribe(subscription)
		select {
		case <-eventFetched:
		case <-ctx.Done():
		}
	})

	service.Subscribe(subscription)

	_, err := service.Start(context.Background(), group)
	require.NoError(t, err)

	service.Resume()
	group.Wait()
}

func TestService_WaitOnEventPublishAfterPause(t *testing.T) {
	group := orderedtasks.NewOrderedCancelGroup(async.NoopPanicHandler{})
	mockCtrl := gomock.NewController(t)
	eventPublisher := mocks2.NewMockEventPublisher(mockCtrl)
	eventIDStore := mocks.NewMockEventIDStore(mockCtrl)
	eventSource := mocks.NewMockEventSource(mockCtrl)
	subscriber := NewMockMessageEventHandler(mockCtrl)

	firstEventID := "EVENT01"
	secondEventID := "EVENT02"
	messageEvents := []proton.MessageEvent{
		{
			EventItem: proton.EventItem{ID: "Message"},
		},
	}
	secondEvent := []proton.Event{{
		EventID:  secondEventID,
		Messages: messageEvents,
	}}

	// Event id store expectations.
	eventIDStore.EXPECT().Load(gomock.Any()).Times(1).Return(firstEventID, nil)
	eventIDStore.EXPECT().Store(gomock.Any(), gomock.Eq(secondEventID)).Times(1).Return(nil)

	// Event Source expectations.
	eventSource.EXPECT().GetEvent(gomock.Any(), gomock.Eq(firstEventID)).MinTimes(1).Return(secondEvent, false, nil)

	// Subscriber expectations.

	service := NewService(
		"foo",
		eventSource,
		eventIDStore,
		eventPublisher,
		time.Millisecond,
		time.Millisecond,
		time.Second,
		async.NoopPanicHandler{},
		events.NewNullSubscription(),
		sentry.NullSentryReporter{},
	)
	defer func() { group.CancelAndWait(); service.Close() }()

	waiters := make(chan *EventPollWaiter, 1)
	subscriber.EXPECT().HandleMessageEvents(gomock.Any(), gomock.Eq(messageEvents)).Times(1).DoAndReturn(func(_ context.Context, _ []proton.MessageEvent) error {
		waiters <- service.PauseWithWaiter()
		return nil
	})

	service.Subscribe(NewCallbackSubscriber("foo", EventHandler{MessageHandler: subscriber}))

	_, err := service.Start(context.Background(), group)
	require.NoError(t, err)

	service.Resume()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, (<-waiters).WaitPollFinished(ctx))
	group.CancelAndWait()
}

func TestService_EventRewind(t *testing.T) {
	group := orderedtasks.NewOrderedCancelGroup(async.NoopPanicHandler{})
	mockCtrl := gomock.NewController(t)
	eventPublisher := mocks2.NewMockEventPublisher(mockCtrl)
	eventIDStore := mocks.NewMockEventIDStore(mockCtrl)
	eventSource := mocks.NewMockEventSource(mockCtrl)

	firstEventID := "EVENT01"
	secondEventID := "EVENT02"
	messageEvents := []proton.MessageEvent{
		{
			EventItem: proton.EventItem{ID: "Message"},
		},
	}
	secondEvent := []proton.Event{{
		EventID:  secondEventID,
		Messages: messageEvents,
	}}

	// Return Second Event from id store, but then reset to event 1

	// Event id store expectations.
	store1 := eventIDStore.EXPECT().Load(gomock.Any()).Times(1).Return(secondEventID, nil)
	eventIDStore.EXPECT().Store(gomock.Any(), gomock.Eq(firstEventID)).Times(1).Return(nil).After(store1)
	eventIDStore.EXPECT().Store(gomock.Any(), gomock.Eq(secondEventID)).Times(1).Return(nil)

	// Event Source expectations.
	eventSource.EXPECT().GetEvent(gomock.Any(), gomock.Eq(firstEventID)).MinTimes(1).DoAndReturn(
		func(_ context.Context, _ string) ([]proton.Event, bool, error) {
			group.Cancel()
			return secondEvent, false, nil
		},
	)

	// Subscriber expectations.

	service := NewService(
		"foo",
		eventSource,
		eventIDStore,
		eventPublisher,
		time.Millisecond,
		time.Millisecond,
		time.Second,
		async.NoopPanicHandler{},
		events.NewNullSubscription(),
		sentry.NullSentryReporter{},
	)
	defer func() { group.CancelAndWait(); service.Close() }()

	_, err := service.Start(context.Background(), group)
	require.NoError(t, err)

	require.NoError(t, service.RewindEventID(context.Background(), firstEventID))

	service.Resume()
	group.Wait()
}

type CallbackSubscriber struct {
	handler EventHandler
	n       string
}

func NewCallbackSubscriber(name string, handler EventHandler) *CallbackSubscriber {
	return &CallbackSubscriber{handler: handler, n: name}
}

func (c CallbackSubscriber) name() string { //nolint: unused
	return c.n
}
func (c CallbackSubscriber) handle(ctx context.Context, t proton.Event) error { //nolint: unused
	return c.handler.OnEvent(ctx, t)
}

func (c CallbackSubscriber) cancel() { //nolint: unused
	// Nothing to do.
}

func (c CallbackSubscriber) close() { //nolint: unused
	// Nothing to do.
}
