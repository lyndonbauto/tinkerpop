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
	"crypto/tls"
	"sync"
)

type connectionState int

const (
	initialized connectionState = iota + 1
	established
	closed
	closedDueToError
)

type connection struct {
	logHandler *logHandler
	protocol   protocol
	results    *synchronizedMap
	state      connectionState
}

func (connection *connection) errorCallback() {
	connection.logHandler.log(Error, errorCallback)
	connection.state = closedDueToError
	_ = connection.protocol.close()
}

func (connection *connection) close() error {
	if connection.state != established {
		return NewError(err0101ConnectionCloseError)
	}
	connection.logHandler.log(Info, closeConnection)
	var err error
	if connection.protocol != nil {
		err = connection.protocol.close()
	}
	connection.state = closed
	return err
}

func (connection *connection) write(request *request) (ResultSet, error) {
	connection.logHandler.log(Info, writeRequest)
	requestID := request.requestID.String()
	connection.logHandler.logf(Info, creatingRequest, requestID)
	resultSet := newChannelResultSet(requestID, connection.results)
	connection.results.store(requestID, resultSet)
	return resultSet, connection.protocol.write(request)
}

func (connection *connection) activeResults() int {
	return connection.results.size()
}

func createConnection(url string, authInfo *AuthInfo, tlsConfig *tls.Config, logHandler *logHandler) (*connection, error) {
	conn := &connection{
		logHandler,
		nil,
		&synchronizedMap{map[string]ResultSet{}, sync.Mutex{}},
		initialized,
	}
	logHandler.log(Info, connectConnection)
	protocol, err := newGremlinServerWSProtocol(logHandler, Gorilla, url, authInfo, tlsConfig, conn.results, conn.errorCallback)
	if err != nil {
		logHandler.logf(Error, failedConnection)
		conn.state = closedDueToError
		return nil, err
	}
	conn.protocol = protocol
	conn.state = established
	return conn, err
}

type synchronizedMap struct {
	internalMap map[string]ResultSet
	syncLock    sync.Mutex
}

func (s *synchronizedMap) store(key string, value ResultSet) {
	s.syncLock.Lock()
	defer s.syncLock.Unlock()
	s.internalMap[key] = value
}

func (s *synchronizedMap) load(key string) ResultSet {
	s.syncLock.Lock()
	defer s.syncLock.Unlock()
	return s.internalMap[key]
}

func (s *synchronizedMap) delete(key string) {
	s.syncLock.Lock()
	defer s.syncLock.Unlock()
	delete(s.internalMap, key)
}

func (s *synchronizedMap) size() int {
	s.syncLock.Lock()
	defer s.syncLock.Unlock()
	return len(s.internalMap)
}

func (s *synchronizedMap) synchronizedRange(f func(key string, value ResultSet)) {
	s.syncLock.Lock()
	defer s.syncLock.Unlock()
	for k, v := range s.internalMap {
		f(k, v)
	}
}
