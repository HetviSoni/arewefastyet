/*
 *
 * Copyright 2021 The Vitess Authors.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 * /
 */

package server

import (
	"path"
	"sync"
	"time"

	"github.com/robfig/cron/v3"
)

type (
	executionQueueElement struct {
		config                  string
		retry                   int
		identifier              executionIdentifier
		compareWith             []executionIdentifier
		notifyAlways, Executing bool
	}

	executionIdentifier struct {
		GitRef, Source, BenchmarkType, PlannerVersion string
		PullNb                                        int
		PullBaseRef                                   string
	}

	executionQueue map[executionIdentifier]*executionQueueElement
)

const (
	// maxConcurJob is the maximum number of concurrent jobs that we can execute
	maxConcurJob = 1
)

var (
	currentCountExec int
	mtx              sync.RWMutex
	queue            executionQueue
)

func createIndividualCron(schedule string, job func()) error {
	if schedule == "" {
		return nil
	}

	c := cron.New()
	_, err := c.AddFunc(schedule, job)
	if err != nil {
		return err
	}
	c.Start()
	return nil
}

func (s *Server) createCrons() error {
	queue = make(executionQueue)

	crons := []struct {
		schedule string
		f        func()
		name     string
	}{
		{name: "branch", schedule: s.cronSchedule, f: s.branchCronHandler},
		{name: "pull_requests", schedule: s.cronSchedulePullRequests, f: s.pullRequestsCronHandler},
		{name: "tags", schedule: s.cronScheduleTags, f: s.tagsCronHandler},
	}
	for _, c := range crons {
		if c.schedule == "none" {
			continue
		}
		slog.Info("Starting the CRON ", c.name, " with schedule: ", c.schedule)
		err := createIndividualCron(c.schedule, c.f)
		if err != nil {
			return err
		}
	}
	go s.cronExecutionQueueWatcher()
	return nil
}

func (s *Server) getConfigFiles() map[string]string {
	configs := map[string]string{
		"micro": path.Join(s.benchmarkConfigPath, "micro.yaml"),
		"oltp":  path.Join(s.benchmarkConfigPath, "oltp.yaml"),
		"tpcc":  path.Join(s.benchmarkConfigPath, "tpcc.yaml"),
	}
	return configs
}

func (s *Server) addToQueue(element *executionQueueElement) {
	mtx.Lock()
	defer func() {
		mtx.Unlock()
	}()

	_, found := queue[element.identifier]

	if found {
		return
	}
	exists, err := s.checkIfExecutionExists(element.identifier)
	if err != nil {
		slog.Error(err.Error())
		return
	}
	if !exists {
		queue[element.identifier] = element
		slog.Infof("%+v is added to the queue", element.identifier)

		// we sleep here to avoid adding too many similar elements to the queue at the same time.
		time.Sleep(2 * time.Second)
	}
}
