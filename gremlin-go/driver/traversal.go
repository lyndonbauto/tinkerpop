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

type Traverser struct {
	bulk  int64
	value interface{}
}

type Traversal struct {
	graph               *Graph
	traversalStrategies *TraversalStrategies
	bytecode            *bytecode
	remote              *DriverRemoteConnection
	results             *ResultSet
}

// ToList returns the result in a list.
func (t *Traversal) ToList() ([]*Result, error) {
	results, err := t.remote.SubmitBytecode(t.bytecode)
	if err != nil {
		return nil, err
	}
	return results.All(), nil
}

// ToSet returns the results in a set.
func (t *Traversal) ToSet() (map[*Result]bool, error) {
	list, err := t.ToList()
	if err != nil {
		return nil, err
	}

	set := map[*Result]bool{}
	for _, r := range list {
		set[r] = true
	}
	return set, nil
}

// Iterate all the Traverser instances in the traversal and returns the empty traversal
func (t *Traversal) Iterate() (*Traversal, <-chan bool, error) {
	err := t.bytecode.addStep("none")
	if err != nil {
		return nil, nil, err
	}

	res, err := t.remote.SubmitBytecode(t.bytecode)
	if err != nil {
		return nil, nil, err
	}

	r := make(chan bool)
	go func() {
		defer close(r)

		// Force waiting until complete.
		_ = res.All()
		r <- true
	}()

	return t, r, nil
}

func (t *Traversal) HasNext() (bool, error) {
	results, err := t.getResults()
	if err != nil {
		return false, err
	}
	return !(*results).IsEmpty(), nil
}

func (t *Traversal) Next() (*Result, error) {
	results, err := t.getResults()
	if err != nil || (*results).IsEmpty() {
		return nil, err
	}
	return (*results).one(), nil
}

func (t *Traversal) getResults() (*ResultSet, error) {
	var err error = nil
	if t.results == nil {
		var results ResultSet
		results, err = t.remote.SubmitBytecode(t.bytecode)
		t.results = &results
	}
	return t.results, err
}

type Barrier string

const (
	NormSack Barrier = "normSack"
)

type Cardinality string

const (
	Single Cardinality = "single"
	List   Cardinality = "list_"
	Set    Cardinality = "set_"
)

type Column string

const (
	Keys   Column = "keys"
	Values Column = "values"
)

type Direction string

const (
	In   Direction = "IN"
	Out  Direction = "OUT"
	Both Direction = "BOTH"
)

type Order string

const (
	Shuffle Order = "shuffle"
	Asc     Order = "asc"
	Desc    Order = "desc"
)

type Pick string

const (
	Any  Pick = "any"
	None Pick = "none"
)

type Pop string

const (
	First Pop = "first"
	Last  Pop = "last"
	All   Pop = "all_"
	Mixed Pop = "mixed"
)

type Scope string

const (
	Global Scope = "global_"
	Local  Scope = "local"
)

type T string

const (
	Id    T = "id"
	Label T = "label"
	Id_   T = "id_"
	Key   T = "key"
	Value T = "value"
)

type Operator string

const (
	Sum     Operator = "sum_"
	Sum_    Operator = "sum"
	Minus   Operator = "minus"
	Mult    Operator = "mult"
	Div     Operator = "div"
	Min     Operator = "min"
	Min_    Operator = "min_"
	Max_    Operator = "max_"
	Assign  Operator = "assign"
	And_    Operator = "and_"
	Or_     Operator = "or_"
	AddAll  Operator = "addAll"
	SumLong Operator = "sumLong"
)
