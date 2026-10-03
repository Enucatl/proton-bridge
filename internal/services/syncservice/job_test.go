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

package syncservice

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/ProtonMail/gluon/async"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"
	"go.uber.org/mock/gomock"
)

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}

func TestJob_WaitsOnChildren(t *testing.T) {
	mockCtrl := gomock.NewController(t)

	tj := newTestJob(t, context.Background(), mockCtrl, "u", getTestLabels())

	tj.state.EXPECT().SetLastMessageID(gomock.Any(), gomock.Eq("1"), gomock.Eq(int64(0))).Return(nil)
	tj.state.EXPECT().SetLastMessageID(gomock.Any(), gomock.Eq("2"), gomock.Eq(int64(1))).Return(nil)
	tj.syncReporter.EXPECT().OnProgress(gomock.Any(), gomock.Any()).Times(2)

	var workers sync.WaitGroup
	t.Cleanup(workers.Wait)
	workers.Go(func() {
		tj.job.begin()
		job1 := tj.job.newChildJob("1", 0)
		job2 := tj.job.newChildJob("2", 1)

		job1.onFinished(context.Background())
		job2.onFinished(context.Background())
		tj.job.end()
	})

	require.NoError(t, tj.job.waitAndClose(context.Background()))
}

func TestJob_WaitsOnAllChildrenOnError(t *testing.T) {
	mockCtrl := gomock.NewController(t)

	tj := newTestJob(t, context.Background(), mockCtrl, "u", getTestLabels())

	tj.state.EXPECT().SetLastMessageID(gomock.Any(), gomock.Eq("1"), gomock.Eq(int64(0))).Return(nil)
	tj.syncReporter.EXPECT().OnProgress(gomock.Any(), gomock.Any())

	jobErr := errors.New("failed")

	startCh := make(chan struct{})

	var workers sync.WaitGroup
	t.Cleanup(workers.Wait)
	workers.Go(func() {
		job1 := tj.job.newChildJob("1", 0)
		job2 := tj.job.newChildJob("2", 1)

		<-startCh

		job1.onFinished(context.Background())
		job2.onError(jobErr)
		tj.job.end()
	})

	close(startCh)
	err := tj.job.waitAndClose(context.Background())
	require.Error(t, err)
	require.ErrorIs(t, err, jobErr)
}

func TestJob_MultipleChildrenReportError(t *testing.T) {
	mockCtrl := gomock.NewController(t)

	tj := newTestJob(t, context.Background(), mockCtrl, "u", getTestLabels())

	jobErr := errors.New("failed")

	startCh := make(chan struct{})

	wg := sync.WaitGroup{}
	var workers sync.WaitGroup
	t.Cleanup(workers.Wait)
	for range 10 {
		wg.Add(1)
		workers.Go(func() {
			job := tj.job.newChildJob("1", 0)
			wg.Done()
			<-startCh
			job.onError(jobErr)
		})
	}

	wg.Wait()
	tj.job.end()
	close(startCh)
	err := tj.job.waitAndClose(context.Background())
	require.Error(t, err)
	require.ErrorIs(t, err, jobErr)
}

func TestJob_ChildFailureCancelsAllOtherChildJobs(t *testing.T) {
	mockCtrl := gomock.NewController(t)

	tj := newTestJob(t, context.Background(), mockCtrl, "u", getTestLabels())

	jobErr := errors.New("failed")

	failJob := tj.job.newChildJob("0", 1)

	tj.job.begin()
	var workers sync.WaitGroup
	t.Cleanup(func() { tj.job.cancel(); workers.Wait() })
	cancelled := make([]bool, 10)
	for i := range cancelled {
		job := tj.job.newChildJob("1", 0)
		workers.Go(func() {
			<-job.getContext().Done()
			cancelled[i] = job.checkCancelled()
		})
	}
	failJob.onError(jobErr)
	workers.Wait()
	tj.job.end()

	err := tj.job.waitAndClose(context.Background())
	require.Error(t, err)
	require.ErrorIs(t, err, jobErr)
	for _, wasCancelled := range cancelled {
		require.True(t, wasCancelled)
	}
}

func TestJob_CtxCancelCancelsAllChildren(t *testing.T) {
	mockCtrl := gomock.NewController(t)

	ctx, cancel := context.WithCancel(context.Background())
	tj := newTestJob(t, ctx, mockCtrl, "u", getTestLabels())

	var workers sync.WaitGroup
	t.Cleanup(func() { cancel(); workers.Wait() })
	cancelled := make([]bool, 10)
	for i := range cancelled {
		job := tj.job.newChildJob("1", 0)
		workers.Go(func() {
			<-job.getContext().Done()
			cancelled[i] = job.checkCancelled()
		})
	}

	tj.job.end()
	cancel()

	err := tj.job.waitAndClose(ctx)
	require.Error(t, err)
	require.ErrorIs(t, err, context.Canceled)
	workers.Wait()
	for _, wasCancelled := range cancelled {
		require.True(t, wasCancelled)
	}
}

func TestJob_CtxCancelBeforeBegin(t *testing.T) {
	mockCtrl := gomock.NewController(t)

	ctx, cancel := context.WithCancel(context.Background())
	tj := newTestJob(t, ctx, mockCtrl, "u", getTestLabels())

	cancel()
	tj.job.end()
	err := tj.job.waitAndClose(ctx)
	require.Error(t, err)
	require.ErrorIs(t, err, context.Canceled)
}

func TestJob_WithoutChildJobsCanBeTerminated(t *testing.T) {
	mockCtrl := gomock.NewController(t)

	ctx := context.Background()

	tj := newTestJob(t, ctx, mockCtrl, "u", getTestLabels())
	var workers sync.WaitGroup
	t.Cleanup(workers.Wait)
	workers.Go(func() {
		tj.job.begin()
		tj.job.end()
	})
	err := tj.job.waitAndClose(context.Background())
	require.NoError(t, err)
}

type tjob struct {
	job            *Job
	client         *MockAPIClient
	messageBuilder *MockMessageBuilder
	updateApplier  *MockUpdateApplier
	syncReporter   *MockReporter
	state          *MockStateProvider
}

func newTestJob(
	t *testing.T,
	ctx context.Context,
	mockCtrl *gomock.Controller,
	userID string,
	labels LabelMap,
) tjob {
	client := NewMockAPIClient(mockCtrl)
	messageBuilder := NewMockMessageBuilder(mockCtrl)
	updateApplier := NewMockUpdateApplier(mockCtrl)
	syncReporter := NewMockReporter(mockCtrl)
	state := NewMockStateProvider(mockCtrl)

	job := NewJob(
		ctx,
		client,
		userID,
		labels,
		messageBuilder,
		updateApplier,
		syncReporter,
		state,
		&async.NoopPanicHandler{},
		newDownloadCache(),
		logrus.WithField("s", "test"),
	)
	cleanupTestJob(t, job)

	return tjob{
		job:            job,
		client:         client,
		messageBuilder: messageBuilder,
		updateApplier:  updateApplier,
		syncReporter:   syncReporter,
		state:          state,
	}
}

// Register before stage workers so they stop before the waiter and mocks are cleaned up.
func cleanupTestJob(t *testing.T, job *Job) {
	t.Helper()
	t.Cleanup(func() {
		job.cancel()
		select {
		case <-job.jw.doneCh:
		default:
			job.close()
		}
		for range job.jw.doneCh {
		}
	})
}
