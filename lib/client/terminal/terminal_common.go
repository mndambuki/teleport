/*
Copyright 2021 Gravitational, Inc.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/
package terminal

import (
	"sync"

	"github.com/gravitational/teleport"
	"github.com/gravitational/trace"
	"github.com/sirupsen/logrus"
)

var log = logrus.WithFields(logrus.Fields{
	trace.Component: teleport.ComponentClient,
})

// ResizeEvent is emitted when a terminal window is resized.
type ResizeEvent struct{}

// StopEvent is emitted when the user sends a SIGSTOP
type StopEvent struct{}

type signalEmitter struct {
	subscribers      []chan interface{}
	subscribersMutex sync.Mutex
}

// Subscribe creates a channel that will receive terminal events.
func (e *signalEmitter) Subscribe() chan interface{} {
	e.subscribersMutex.Lock()
	defer e.subscribersMutex.Unlock()

	ch := make(chan interface{})
	e.subscribers = append(e.subscribers, ch)

	return ch
}

func (e *signalEmitter) writeEvent(event interface{}) {
	e.subscribersMutex.Lock()
	defer e.subscribersMutex.Unlock()

	for _, sub := range e.subscribers {
		sub <- event
	}
}

func (e *signalEmitter) clearSubscribers() {
	e.subscribersMutex.Lock()
	defer e.subscribersMutex.Unlock()

	for _, ch := range e.subscribers {
		close(ch)
	}
	e.subscribers = e.subscribers[:0]
}
