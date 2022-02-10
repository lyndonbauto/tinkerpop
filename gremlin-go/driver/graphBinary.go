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
	"bytes"
	"encoding/binary"
	"errors"
	"math/big"
	"reflect"

	"github.com/google/uuid"
)

// Version 1.0

// DataType graphbinary types
type DataType uint8

// DataType defined as constants
const (
	NullType       DataType = 0xFE
	IntType        DataType = 0x01
	LongType       DataType = 0x02
	StringType     DataType = 0x03
	DoubleType     DataType = 0x07
	FloatType      DataType = 0x08
	ListType       DataType = 0x09
	MapType        DataType = 0x0a
	UUIDType       DataType = 0x0c
	BytecodeType   DataType = 0x15
	TraverserType  DataType = 0x21
	ByteType       DataType = 0x24
	ShortType      DataType = 0x26
	BooleanType    DataType = 0x27
	BigIntegerType DataType = 0x23
)

var nullBytes = []byte{NullType.getCodeByte(), 0x01}

func (dataType DataType) getCodeByte() byte {
	return byte(dataType)
}

func (dataType DataType) getCodeBytes() []byte {
	return []byte{dataType.getCodeByte()}
}

// graphBinaryTypeSerializer struct for the different types of serializers
type graphBinaryTypeSerializer struct {
	dataType       DataType
	writer         func(interface{}, *bytes.Buffer, *graphBinaryTypeSerializer) ([]byte, error)
	reader         func(*bytes.Buffer, *graphBinaryTypeSerializer) (interface{}, error)
	nullFlagReturn interface{}
	logHandler     *logHandler
}

func (serializer *graphBinaryTypeSerializer) writeType(value interface{}, buffer *bytes.Buffer, typeSerializer *graphBinaryTypeSerializer) ([]byte, error) {
	return serializer.writeTypeValue(value, buffer, typeSerializer, true)
}

func (serializer *graphBinaryTypeSerializer) writeTypeValue(value interface{}, buffer *bytes.Buffer, typeSerializer *graphBinaryTypeSerializer, nullable bool) ([]byte, error) {
	if value == nil {
		if !nullable {
			serializer.logHandler.log(Error, unexpectedNull)
			return nil, errors.New("unexpected null value to writeType when nullable is false")
		}
		serializer.writeValueFlagNull(buffer)
		return buffer.Bytes(), nil
	}
	if nullable {
		serializer.writeValueFlagNone(buffer)
	}
	return typeSerializer.writer(value, buffer, typeSerializer)
}

func (serializer graphBinaryTypeSerializer) readType(buffer *bytes.Buffer, typeSerializer *graphBinaryTypeSerializer) (interface{}, error) {
	return serializer.readTypeValue(buffer, typeSerializer, true)
}

func (serializer graphBinaryTypeSerializer) readTypeValue(buffer *bytes.Buffer, typeSerializer *graphBinaryTypeSerializer, nullable bool) (interface{}, error) {
	if nullable {
		nullFlag, err := buffer.ReadByte()
		if err != nil {
			return nil, err
		}
		if nullFlag == valueFlagNull {
			return serializer.nullFlagReturn, nil
		}
	}
	return typeSerializer.reader(buffer, typeSerializer)
}

// Format: {length}{item_0}...{item_n}
func listWriter(value interface{}, buffer *bytes.Buffer, typeSerializer *graphBinaryTypeSerializer) ([]byte, error) {
	v := reflect.ValueOf(value)
	if (v.Kind() != reflect.Array) && (v.Kind() != reflect.Slice) {
		typeSerializer.logHandler.log(Error, notSlice)
		return buffer.Bytes(), errors.New("did not get the expected array or slice type as input")
	}
	valLen := v.Len()
	err := binary.Write(buffer, binary.BigEndian, int32(valLen))
	if err != nil {
		return nil, err
	}
	if valLen < 1 {
		return buffer.Bytes(), nil
	}
	for i := 0; i < valLen; i++ {
		_, err := typeSerializer.write(v.Index(i).Interface(), buffer)
		if err != nil {
			return nil, err
		}
	}
	return buffer.Bytes(), nil
}

func listReader(buffer *bytes.Buffer, typeSerializer *graphBinaryTypeSerializer) (interface{}, error) {
	var size int32
	err := binary.Read(buffer, binary.BigEndian, &size)
	if err != nil {
		return nil, err
	}
	// Currently, all list data types will be converted to a slice of interface{}.
	var valList []interface{}
	for i := 0; i < int(size); i++ {
		val, err := typeSerializer.read(buffer)
		if err != nil {
			return nil, err
		}
		valList = append(valList, val)
	}
	return valList, nil
}

// Format: {length}{item_0}...{item_n}
func mapWriter(value interface{}, buffer *bytes.Buffer, typeSerializer *graphBinaryTypeSerializer) ([]byte, error) {
	v := reflect.ValueOf(value)
	if v.Kind() != reflect.Map {
		typeSerializer.logHandler.log(Error, notMap)
		return buffer.Bytes(), errors.New("did not get the expected map type as input")
	}

	keys := v.MapKeys()
	err := binary.Write(buffer, binary.BigEndian, int32(len(keys)))
	if err != nil {
		return nil, err
	}
	for _, k := range keys {
		convKey := k.Convert(v.Type().Key())
		// serialize k
		_, err := typeSerializer.write(k.Interface(), buffer)
		if err != nil {
			return nil, err
		}
		// serialize v.MapIndex(c_key)
		val := v.MapIndex(convKey)
		_, err = typeSerializer.write(val.Interface(), buffer)
		if err != nil {
			return nil, err
		}

	}
	return buffer.Bytes(), nil
}

func mapReader(buffer *bytes.Buffer, typeSerializer *graphBinaryTypeSerializer) (interface{}, error) {
	var size int32
	err := binary.Read(buffer, binary.BigEndian, &size)
	if err != nil {
		return nil, err
	}
	// Currently, all map data types will be converted to a map of [interface{}]interface{}.
	valMap := make(map[interface{}]interface{})
	for i := 0; i < int(size); i++ {
		key, err := typeSerializer.read(buffer)
		if err != nil {
			return nil, err
		}
		val, err := typeSerializer.read(buffer)
		if err != nil {
			return nil, err
		}
		valMap[key] = val
	}
	return valMap, nil
}

func instructionSetWriter(instructions []instruction, buffer *bytes.Buffer, typeSerializer *graphBinaryTypeSerializer) error {
	// Write {steps_length}, i.e number of steps.
	err := binary.Write(buffer, binary.BigEndian, int32(len(instructions)))
	if err != nil {
		return err
	}

	// Write {step_0} to {step_n}.
	for i := range instructions {
		// Write {name} of {step_i}.
		// Note: {name} follows string writing, therefore write string length followed by actual string.
		_, err = typeSerializer.writeValue(instructions[i].operator, buffer, false)
		if err != nil {
			return err
		}

		// Write {values_length} of {step_i}.
		err = binary.Write(buffer, binary.BigEndian, int32(len(instructions[i].arguments)))
		if err != nil {
			return err
		}

		// Write {values_0} to {values_n}.
		for j := range instructions[i].arguments {
			_, err = typeSerializer.write(instructions[i].arguments[j], buffer)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

// Format: {steps_length}{step_0}…{step_n}{sources_length}{source_0}…{source_n}
// Where:
//		{steps_length} is an Int value describing the amount of steps.
//		{step_i} is composed of {name}{values_length}{value_0}…{value_n}, where:
//      {name} is a String. This is also known as the operator.
//		{values_length} is an Int describing the amount values.
//		{value_i} is a fully qualified typed value composed of {type_code}{type_info}{value_flag}{value} describing the step argument.
func bytecodeWriter(value interface{}, buffer *bytes.Buffer, typeSerializer *graphBinaryTypeSerializer) ([]byte, error) {
	bc := value.(bytecode)

	// Write {steps_length} and {step_0} through {step_n}, then {sources_length} and {source_0} through {source_n}
	err := instructionSetWriter(bc.stepInstructions, buffer, typeSerializer)
	if err != nil {
		return nil, err
	}
	err = instructionSetWriter(bc.sourceInstructions, buffer, typeSerializer)
	if err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

// Golang stores BigIntegers with big.Int types
// it contains an unsigned representation of the number and uses a boolean to track +ve and -ve
// getSignedBytesFromBigInt gives us the signed(two's complement) byte array that represents the unsigned byte array in
// big.Int
func getSignedBytesFromBigInt(n *big.Int) []byte {
	var one = big.NewInt(1)
	if n.Sign() == 1 {
		// add a buffer 0x00 byte to the start of byte array if number is positive and has a 1 in its MSB
		b := n.Bytes()
		if b[0]&0x80 > 0 {
			b = append([]byte{0}, b...)
		}
		return b
	} else if n.Sign() == -1 {
		// Convert Unsigned byte array to signed byte array
		length := uint(n.BitLen()/8+1) * 8
		b := new(big.Int).Add(n, new(big.Int).Lsh(one, length)).Bytes()
		// Strip any redundant 0xff bytes from the front of the byte array if the following byte starts with a 1
		if len(b) >= 2 && b[0] == 0xff && b[1]&0x80 != 0 {
			b = b[1:]
		}
		return b
	}
	return []byte{}
}

func getBigIntFromSignedBytes(b []byte) *big.Int {
	var newBigInt = big.NewInt(0).SetBytes(b)
	var one = big.NewInt(1)
	if len(b) == 0 {
		return newBigInt
	}
	// If the first bit in the first element of the byte array is a 1, we need to interpret the byte array as a two's complement representation
	if b[0]&0x80 == 0x00 {
		newBigInt.SetBytes(b)
		return newBigInt
	}
	// Undo two's complement to byte array and set negative boolean to true
	length := uint((len(b)*8)/8+1) * 8
	b2 := new(big.Int).Sub(newBigInt, new(big.Int).Lsh(one, length)).Bytes()

	// Strip the resulting 0xff byte at the start of array
	b2 = b2[1:]

	// Strip any redundant 0x00 byte at the start of array
	if b2[0] == 0x00 {
		b2 = b2[1:]
	}
	newBigInt = big.NewInt(0)
	newBigInt.SetBytes(b2)
	newBigInt.Neg(newBigInt)
	return newBigInt
}

// Format: {length}{value_0}...{value_n}
func bigIntWriter(value interface{}, buffer *bytes.Buffer, _ *graphBinaryTypeSerializer) ([]byte, error) {
	v := value.(*big.Int)
	signedBytes := getSignedBytesFromBigInt(v)
	err := binary.Write(buffer, binary.BigEndian, int32(len(signedBytes)))
	if err != nil {
		return nil, err
	}
	for i := 0; i < len(signedBytes); i++ {
		err := binary.Write(buffer, binary.BigEndian, signedBytes[i])
		if err != nil {
			return nil, err
		}
	}
	return buffer.Bytes(), nil
}

func bigIntReader(buffer *bytes.Buffer, _ *graphBinaryTypeSerializer) (interface{}, error) {
	var size int32
	err := binary.Read(buffer, binary.BigEndian, &size)
	if err != nil {
		return nil, err
	}
	var valList = make([]byte, size)
	for i := int32(0); i < size; i++ {
		err := binary.Read(buffer, binary.BigEndian, &valList[i])
		if err != nil {
			return nil, err
		}
	}
	return getBigIntFromSignedBytes(valList), nil
}

const (
	valueFlagNull byte = 1
	valueFlagNone byte = 0
)

// Writes an object in fully-qualified format, containing {type_code}{type_info}{value_flag}{value}.
func (serializer *graphBinaryTypeSerializer) write(valueObject interface{}, buffer *bytes.Buffer) (interface{}, error) {
	if valueObject == nil {
		// return Object of type "unspecified object null" with the value flag set to null.
		buffer.Write(nullBytes)
		return buffer.Bytes(), nil
	}

	typeSerializer, err := serializer.getSerializerToWrite(valueObject)
	if err != nil {
		return nil, err
	}
	buffer.Write(typeSerializer.dataType.getCodeBytes())
	message, err := typeSerializer.writeType(valueObject, buffer, typeSerializer)
	if err != nil {
		return nil, err
	}
	return message, nil
}

// Writes a value without including type information.
func (serializer *graphBinaryTypeSerializer) writeValue(value interface{}, buffer *bytes.Buffer, nullable bool) (interface{}, error) {
	if value == nil {
		if !nullable {
			serializer.logHandler.log(Error, unexpectedNull)
			return nil, errors.New("unexpected null value to writeType when nullable is false")
		}
		serializer.writeValueFlagNull(buffer)
		return buffer.Bytes(), nil
	}

	typeSerializer, err := serializer.getSerializerToWrite(value)
	if err != nil {
		return nil, err
	}
	// buffer.Write(typeSerializer.dataType.getCodeBytes())
	message, err := typeSerializer.writeTypeValue(value, buffer, typeSerializer, nullable)
	if err != nil {
		return nil, err
	}
	return message, nil
}

func (serializer *graphBinaryTypeSerializer) writeValueFlagNull(buffer *bytes.Buffer) {
	buffer.WriteByte(valueFlagNull)
}

func (serializer *graphBinaryTypeSerializer) writeValueFlagNone(buffer *bytes.Buffer) {
	buffer.WriteByte(valueFlagNone)
}

// Reads the type code, information and value of a given buffer with fully-qualified format.
func (serializer *graphBinaryTypeSerializer) read(buffer *bytes.Buffer) (interface{}, error) {
	var typeCode DataType
	err := binary.Read(buffer, binary.BigEndian, &typeCode)
	if err != nil {
		return nil, err
	}
	if typeCode == NullType {
		var isNull byte
		_ = binary.Read(buffer, binary.BigEndian, &isNull)
		if isNull != 1 {
			return nil, errors.New("expected isNull check to be true for NullType")
		}
		return nil, nil
	}

	typeSerializer, err := serializer.getSerializerToRead(byte(typeCode))
	if err != nil {
		return nil, err
	}
	return typeSerializer.readType(buffer, typeSerializer)
}

func (serializer *graphBinaryTypeSerializer) readValue(buffer *bytes.Buffer, typ byte, nullable bool) (interface{}, error) {
	if buffer == nil {
		serializer.logHandler.log(Error, nullInput)
		return nil, errors.New("input cannot be null")
	}
	typeCode, err := buffer.ReadByte()
	if err != nil {
		return nil, err
	}
	if typeCode != typ {
		serializer.logHandler.logf(Error, unmatchedDataType)
		return nil, errors.New("datatype readType from input buffer different from requested datatype")
	}
	typeSerializer, _ := serializer.getSerializerToRead(typ)
	val, _ := typeSerializer.readTypeValue(buffer, typeSerializer, nullable)
	return val, nil
}

// gets the type of the serializer based on the DataType byte value
func (serializer *graphBinaryTypeSerializer) getSerializerToRead(typ byte) (*graphBinaryTypeSerializer, error) {
	switch typ {
	case TraverserType.getCodeByte():
		{
			return &graphBinaryTypeSerializer{dataType: TraverserType, writer: nil, reader: func(buffer *bytes.Buffer, typeSerializer *graphBinaryTypeSerializer) (interface{}, error) {
				traverser := Traverser{}
				err := binary.Read(buffer, binary.BigEndian, &traverser.bulk)
				if err != nil {
					return nil, err
				}
				traverser.value, err = serializer.read(buffer)
				if err != nil {
					return nil, err
				}
				return traverser, err
			}, nullFlagReturn: "", logHandler: serializer.logHandler}, nil
		}
	case StringType.getCodeByte():
		return &graphBinaryTypeSerializer{dataType: StringType, writer: nil, reader: func(buffer *bytes.Buffer, typeSerializer *graphBinaryTypeSerializer) (interface{}, error) {
			var size int32
			err := binary.Read(buffer, binary.BigEndian, &size)
			if err != nil {
				return nil, err
			}
			valBytes := make([]byte, size)
			_, err = buffer.Read(valBytes)
			return string(valBytes), err
		}, nullFlagReturn: "", logHandler: serializer.logHandler}, nil
	case BigIntegerType.getCodeByte():
		return &graphBinaryTypeSerializer{dataType: BigIntegerType, writer: nil, reader: bigIntReader, nullFlagReturn: 0}, nil
	case LongType.getCodeByte():
		return &graphBinaryTypeSerializer{dataType: LongType, writer: nil, reader: func(buffer *bytes.Buffer, typeSerializer *graphBinaryTypeSerializer) (interface{}, error) {
			var val int64
			return val, binary.Read(buffer, binary.BigEndian, &val)
		}, nullFlagReturn: 0, logHandler: serializer.logHandler}, nil
	case IntType.getCodeByte():
		return &graphBinaryTypeSerializer{dataType: IntType, writer: nil, reader: func(buffer *bytes.Buffer, typeSerializer *graphBinaryTypeSerializer) (interface{}, error) {
			var val int32
			return val, binary.Read(buffer, binary.BigEndian, &val)
		}, nullFlagReturn: 0, logHandler: serializer.logHandler}, nil
	case ShortType.getCodeByte():
		return &graphBinaryTypeSerializer{dataType: ShortType, writer: nil, reader: func(buffer *bytes.Buffer, typeSerializer *graphBinaryTypeSerializer) (interface{}, error) {
			var val int16
			return val, binary.Read(buffer, binary.BigEndian, &val)
		}, nullFlagReturn: 0, logHandler: serializer.logHandler}, nil
	case ByteType.getCodeByte():
		return &graphBinaryTypeSerializer{dataType: ByteType, writer: nil, reader: func(buffer *bytes.Buffer, typeSerializer *graphBinaryTypeSerializer) (interface{}, error) {
			var val uint8
			err := binary.Read(buffer, binary.BigEndian, &val)
			return val, err
		}, nullFlagReturn: 0, logHandler: serializer.logHandler}, nil
	case BooleanType.getCodeByte():
		return &graphBinaryTypeSerializer{dataType: BooleanType, writer: nil, reader: func(buffer *bytes.Buffer, typeSerializer *graphBinaryTypeSerializer) (interface{}, error) {
			var val bool
			err := binary.Read(buffer, binary.BigEndian, &val)
			return val, err
		}, nullFlagReturn: 0, logHandler: serializer.logHandler}, nil
	case UUIDType.getCodeByte():
		return &graphBinaryTypeSerializer{dataType: UUIDType, writer: nil, reader: func(buffer *bytes.Buffer, typeSerializer *graphBinaryTypeSerializer) (interface{}, error) {
			valBytes := make([]byte, 16)
			_, err := buffer.Read(valBytes)
			if err != nil {
				return uuid.Nil, err
			}
			val, _ := uuid.FromBytes(valBytes)
			return val, nil
		}, nullFlagReturn: uuid.Nil, logHandler: serializer.logHandler}, nil
	case FloatType.getCodeByte():
		return &graphBinaryTypeSerializer{dataType: FloatType, writer: nil, reader: func(buffer *bytes.Buffer, typeSerializer *graphBinaryTypeSerializer) (interface{}, error) {
			var val float32
			err := binary.Read(buffer, binary.BigEndian, &val)
			return val, err
		}, nullFlagReturn: 0, logHandler: serializer.logHandler}, nil
	case DoubleType.getCodeByte():
		return &graphBinaryTypeSerializer{dataType: DoubleType, writer: nil, reader: func(buffer *bytes.Buffer, typeSerializer *graphBinaryTypeSerializer) (interface{}, error) {
			var val float64
			err := binary.Read(buffer, binary.BigEndian, &val)
			return val, err
		}, nullFlagReturn: 0, logHandler: serializer.logHandler}, nil
	case MapType.getCodeByte():
		return &graphBinaryTypeSerializer{dataType: MapType, writer: mapWriter, reader: mapReader, nullFlagReturn: nil, logHandler: serializer.logHandler}, nil
	case ListType.getCodeByte():
		return &graphBinaryTypeSerializer{dataType: IntType, writer: listWriter, reader: listReader, nullFlagReturn: nil, logHandler: serializer.logHandler}, nil
	default:
		serializer.logHandler.logf(Error, deserializeDataTypeError, int32(typ))
		return nil, errors.New("unknown data type to deserialize")
	}
}

// gets the type of the serializer based on the value
func (serializer *graphBinaryTypeSerializer) getSerializerToWrite(val interface{}) (*graphBinaryTypeSerializer, error) {
	switch val.(type) {
	case bytecode:
		return &graphBinaryTypeSerializer{dataType: BytecodeType, writer: bytecodeWriter, reader: nil, nullFlagReturn: "", logHandler: serializer.logHandler}, nil
	case string:
		return &graphBinaryTypeSerializer{dataType: StringType, writer: func(value interface{}, buffer *bytes.Buffer, typeSerializer *graphBinaryTypeSerializer) ([]byte, error) {
			err := binary.Write(buffer, binary.BigEndian, int32(len(value.(string))))
			if err != nil {
				return nil, err
			}
			_, err = buffer.WriteString(value.(string))
			return buffer.Bytes(), err
		}, reader: nil, nullFlagReturn: "", logHandler: serializer.logHandler}, nil
	case *big.Int:
		return &graphBinaryTypeSerializer{dataType: BigIntegerType, writer: bigIntWriter, reader: nil, nullFlagReturn: big.NewInt(0)}, nil
	case int64, int, uint32:
		return &graphBinaryTypeSerializer{dataType: LongType, writer: func(value interface{}, buffer *bytes.Buffer, typeSerializer *graphBinaryTypeSerializer) ([]byte, error) {
			switch v := value.(type) {
			case int:
				value = int64(v)
			case uint32:
				value = int64(v)
			}
			err := binary.Write(buffer, binary.BigEndian, value)
			return buffer.Bytes(), err
		}, reader: nil, nullFlagReturn: 0, logHandler: serializer.logHandler}, nil
	case int32, uint16:
		return &graphBinaryTypeSerializer{dataType: IntType, writer: func(value interface{}, buffer *bytes.Buffer, typeSerializer *graphBinaryTypeSerializer) ([]byte, error) {
			switch v := value.(type) {
			case uint16:
				value = int32(v)
			}
			err := binary.Write(buffer, binary.BigEndian, value.(int32))
			return buffer.Bytes(), err
		}, reader: nil, nullFlagReturn: 0, logHandler: serializer.logHandler}, nil
	case int16:
		return &graphBinaryTypeSerializer{dataType: ShortType, writer: func(value interface{}, buffer *bytes.Buffer, typeSerializer *graphBinaryTypeSerializer) ([]byte, error) {
			err := binary.Write(buffer, binary.BigEndian, value.(int16))
			return buffer.Bytes(), err
		}, reader: nil, nullFlagReturn: 0, logHandler: serializer.logHandler}, nil
	case uint8:
		return &graphBinaryTypeSerializer{dataType: ByteType, writer: func(value interface{}, buffer *bytes.Buffer, typeSerializer *graphBinaryTypeSerializer) ([]byte, error) {
			err := binary.Write(buffer, binary.BigEndian, value.(uint8))
			return buffer.Bytes(), err
		}, reader: nil, nullFlagReturn: 0, logHandler: serializer.logHandler}, nil
	case bool:
		return &graphBinaryTypeSerializer{dataType: ByteType, writer: func(value interface{}, buffer *bytes.Buffer, typeSerializer *graphBinaryTypeSerializer) ([]byte, error) {
			err := binary.Write(buffer, binary.BigEndian, value.(bool))
			return buffer.Bytes(), err
		}, reader: nil, nullFlagReturn: false, logHandler: serializer.logHandler}, nil
	case uuid.UUID:
		return &graphBinaryTypeSerializer{dataType: UUIDType, writer: func(value interface{}, buffer *bytes.Buffer, typeSerializer *graphBinaryTypeSerializer) ([]byte, error) {
			err := binary.Write(buffer, binary.BigEndian, value)
			return buffer.Bytes(), err
		}, reader: nil, nullFlagReturn: uuid.Nil, logHandler: serializer.logHandler}, nil
	case float32:
		return &graphBinaryTypeSerializer{dataType: FloatType, writer: func(value interface{}, buffer *bytes.Buffer, typeSerializer *graphBinaryTypeSerializer) ([]byte, error) {
			err := binary.Write(buffer, binary.BigEndian, value)
			return buffer.Bytes(), err
		}, reader: nil, nullFlagReturn: 0, logHandler: serializer.logHandler}, nil
	case float64:
		return &graphBinaryTypeSerializer{dataType: DoubleType, writer: func(value interface{}, buffer *bytes.Buffer, typeSerializer *graphBinaryTypeSerializer) ([]byte, error) {
			err := binary.Write(buffer, binary.BigEndian, value)
			return buffer.Bytes(), err
		}, reader: nil, nullFlagReturn: 0, logHandler: serializer.logHandler}, nil
	default:
		switch reflect.TypeOf(val).Kind() {
		case reflect.Map:
			return &graphBinaryTypeSerializer{dataType: MapType, writer: mapWriter, reader: mapReader, nullFlagReturn: nil, logHandler: serializer.logHandler}, nil
		case reflect.Array, reflect.Slice:
			// We can write an array or slice into the list datatype.
			return &graphBinaryTypeSerializer{dataType: ListType, writer: listWriter, reader: listReader, nullFlagReturn: nil, logHandler: serializer.logHandler}, nil
		default:
			serializer.logHandler.logf(Error, serializeDataTypeError, reflect.TypeOf(val).Name())
			return nil, errors.New("unknown data type to serialize")
		}
	}
}
