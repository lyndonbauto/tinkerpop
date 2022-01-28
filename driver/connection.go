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

type connection struct {
	host            string
	port            int
	transporterType TransporterType
	logHandler      *logHandler
	transporter     transporter
	protocol        protocol
	results         map[string]ResultSet
}

func (connection *connection) close() (err error) {
	if connection.transporter != nil {
		err = connection.transporter.Close()
	}
	return
}

func (connection *connection) connect() error {
	if connection.transporter != nil {
		closeErr := connection.transporter.Close()
		connection.logHandler.logf(Warning, transportCloseFailed, closeErr)
	}
	connection.protocol = newGremlinServerWSProtocol(connection.logHandler)
	connection.transporter = getTransportLayer(connection.transporterType, connection.host, connection.port)
	err := connection.transporter.Connect()
	if err != nil {
		return err
	}
	connection.protocol.connectionMade(connection.transporter)
	return nil
}

func (connection *connection) write(traversalString string) (ResultSet, error) {
	if connection.transporter == nil || connection.transporter.IsClosed() {
		err := connection.connect()
		if err != nil {
			return nil, err
		}
	}

	// Generate request and insert in map with request id as key attached.
	if connection.results == nil {
		connection.results = map[string]ResultSet{}
	}

	// Write through protocol layer.
	responseId, err := connection.protocol.write(traversalString, connection.results)
	return connection.results[responseId], err
}

func newConnection(host string, port int, transporterType TransporterType, handler *logHandler, transporter transporter,
	protocol protocol, results map[string]ResultSet) *connection {
	return &connection{host, port, transporterType, handler, transporter, protocol, results}
}
