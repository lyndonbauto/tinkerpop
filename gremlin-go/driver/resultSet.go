/*
Licensed to the Apache Software Foundation (ASF) under one
or more contributor license agreements.  See the NOTICE file
distributed with this work for additional information
regarding copyright ownership.  The ASF licenses this file
to you under the Apache License, Version 2.0 (the
"License"); you may not use this file except in compliance
with the License.  You may obtain a copy of the License at

http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing,
software distributed under the License is distributed on an
"AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
KIND, either express or implied.  See the License for the
specific language governing permissions and limitations
under the License.
*/

package gremlingo

import (
	"reflect"
	"sync"
)

const defaultCapacity = 1000

// ResultSet interface to define the functions of a ResultSet.
type ResultSet interface {
	setAggregateTo(val string)
	GetAggregateTo() string
	setStatusAttributes(statusAttributes map[string]interface{})
	GetStatusAttributes() map[string]interface{}
	GetRequestID() string
	IsEmpty() bool
	Close()
	Channel() chan *Result
	addResult(result *Result)
	one() *Result
	All() []*Result
	GetError() error
}

// channelResultSet Channel based implementation of ResultSet.
type channelResultSet struct {
	channel          chan *Result
	requestID        string
	aggregateTo      string
	statusAttributes map[string]interface{}
	closed           bool
	err              error
	waitSignal       chan bool
	channelMux       sync.Mutex
	waitSignalMux    sync.Mutex
}

func (channelResultSet *channelResultSet) waitForSignal() {
	channelResultSet.waitSignalMux.Lock()
	waitSignal := make(chan bool)
	channelResultSet.waitSignal = waitSignal
	channelResultSet.waitSignalMux.Unlock()
	// Technically if we assigned channelResultSet.waitSignal then unlocked, it could be set to nil or
	// overwritten to another channel before we check it, so to be safe, create additional variable and
	// check that instead.
	<-waitSignal
}

func (channelResultSet *channelResultSet) sendSignal() {
	// Lock wait
	channelResultSet.waitSignalMux.Lock()
	if channelResultSet.waitSignal != nil {
		channelResultSet.waitSignal <- true
		channelResultSet.waitSignal = nil
	}
	channelResultSet.waitSignalMux.Unlock()
}

func (channelResultSet *channelResultSet) GetError() error {
	return channelResultSet.err
}

func (channelResultSet *channelResultSet) IsEmpty() bool {
	channelResultSet.channelMux.Lock()
	// If our channel is empty and we have no data in it, wait for signal that the state has been updated.
	if len(channelResultSet.channel) == 0 && !channelResultSet.closed {
		// Unlock ChannelResultSet channel mutex so that it can be updated.
		channelResultSet.channelMux.Unlock()
		channelResultSet.waitForSignal()

		// Call IsEmpty again to avoid messing with more locks.
		return channelResultSet.IsEmpty()
	} else if len(channelResultSet.channel) != 0 {
		// Channel is not empty.
		channelResultSet.channelMux.Unlock()
		return false
	} else {
		// Channel is empty && closed here.
		channelResultSet.channelMux.Unlock()
		return true
	}
}

func (channelResultSet *channelResultSet) Close() {
	if !channelResultSet.closed {
		channelResultSet.channelMux.Lock()
		channelResultSet.closed = true
		close(channelResultSet.channel)
		channelResultSet.sendSignal()
		channelResultSet.channelMux.Unlock()
	}
}

func (channelResultSet *channelResultSet) setAggregateTo(val string) {
	channelResultSet.aggregateTo = val
}

func (channelResultSet *channelResultSet) GetAggregateTo() string {
	return channelResultSet.aggregateTo
}

func (channelResultSet *channelResultSet) setStatusAttributes(val map[string]interface{}) {
	channelResultSet.statusAttributes = val
}

func (channelResultSet *channelResultSet) GetStatusAttributes() map[string]interface{} {
	return channelResultSet.statusAttributes
}

func (channelResultSet *channelResultSet) GetRequestID() string {
	return channelResultSet.requestID
}

func (channelResultSet *channelResultSet) Channel() chan *Result {
	return channelResultSet.channel
}

func (channelResultSet *channelResultSet) one() *Result {
	return <-channelResultSet.channel
}

func (channelResultSet *channelResultSet) All() []*Result {
	var results []*Result
	for result := range channelResultSet.channel {
		results = append(results, result)
	}
	return results
}

func (channelResultSet *channelResultSet) addResult(r *Result) {
	channelResultSet.channelMux.Lock()
	if r.GetType().Kind() == reflect.Array || r.GetType().Kind() == reflect.Slice {
		for _, v := range r.result.([]interface{}) {
			if reflect.TypeOf(v) == reflect.TypeOf(&Traverser{}) {
				channelResultSet.channel <- &Result{(v.(*Traverser)).value}
			} else {
				channelResultSet.channel <- &Result{v}
			}
		}
	} else {
		channelResultSet.channel <- &Result{r.result}
	}
	channelResultSet.channelMux.Unlock()
	channelResultSet.sendSignal()
}

func newChannelResultSetCapacity(requestID string, channelSize int) ResultSet {
	return &channelResultSet{make(chan *Result, channelSize), requestID, "", nil, false, nil, nil, sync.Mutex{}, sync.Mutex{}}
}

func newChannelResultSet(requestID string) ResultSet {
	return newChannelResultSetCapacity(requestID, defaultCapacity)
}
