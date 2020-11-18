/*
 * Copyright 2017 Huawei Technologies Co., Ltd
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *    http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

/*
* Created by on 2017/6/22.
 */

package source

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"unicode"

	"github.com/go-chassis/go-archaius/cast"
	"github.com/go-chassis/openlog"
)

const (
	configClientTag  = `yaml`
	ignoreField      = `ignoredField` // when used -
	doNotConsiderTag = ``
	inline           = "inline"

	fmtValueNotMatched = "value types of %s not matched. expect type : %s, config client type : %s"
)

/*
   unmarshal configurations on supplied object.
   multi level configuration key structure > source.module.type.config: value
   simple key structure > config: value
*/
func (m *Manager) unmarshal(rValue reflect.Value, tagName string) (err error) {
	// handle panic
	defer func() {
		if r := recover(); r != nil {
			msg := fmt.Sprintf("unmarshalling [%s] failed, err: %s", tagName, r.(error).Error())
			err = errors.New(msg)
			openlog.Error(msg)
		}
	}()

	switch rValue.Kind() {
	case reflect.Ptr:
		err := m.handlePtr(rValue, getTagKey(tagName, doNotConsiderTag))
		if err != nil {
			return err
		}

	case reflect.Struct:
		err := m.handleStruct(rValue, getTagKey(tagName, doNotConsiderTag))
		if err != nil {
			return err
		}
	case reflect.Map:
		err := m.handleMap(reflect.Value{}, rValue, getTagKey(tagName, doNotConsiderTag))
		if err != nil {
			return err
		}
	case reflect.String, reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Float32, reflect.Float64, reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32,
		reflect.Uint64, reflect.Bool, reflect.Interface, reflect.Array, reflect.Slice:
		if rValue.CanSet() {
			err := m.setValue(rValue, tagName)
			if err != nil {
				return err
			}
		}
	}

	return nil
}

// handle pointer type objects
func (m *Manager) handlePtr(rValue reflect.Value, tagName string) error {
	if rValue.IsNil() {
		ptrValue := reflect.New(rValue.Type().Elem())
		err := m.unmarshal(ptrValue, getTagKey(tagName, doNotConsiderTag))
		if err != nil {
			return err
		}

		if rValue.CanSet() {
			rValue.Set(ptrValue)
		}
		return nil
	} else if rValue.Elem().Kind() == reflect.Ptr {
		ptrValue := rValue.Elem()
		err := m.handlePtr(ptrValue, getTagKey(tagName, doNotConsiderTag))
		if err != nil {
			return err
		}
	}

	ptrValue := rValue.Elem()
	err := m.unmarshal(ptrValue, getTagKey(tagName, doNotConsiderTag))
	if err != nil {
		return err
	}

	return nil
}

// get multi level configuration key
func getTagKey(currentTag, addTag string) string {
	if currentTag == doNotConsiderTag && addTag == doNotConsiderTag {
		return doNotConsiderTag
	} else if currentTag == doNotConsiderTag && addTag != doNotConsiderTag {
		return addTag
	} else if currentTag != doNotConsiderTag && addTag == doNotConsiderTag {
		return currentTag
	}

	return currentTag + `.` + addTag
}

// handle struct type object
func (m *Manager) handleStruct(rValue reflect.Value, tagName string) error {
	structType := rValue.Type()
	numOfField := structType.NumField()

	for i := 0; i < numOfField; i++ {
		structField := structType.Field(i)
		fieldValue := rValue.Field(i)
		keyName := m.getKeyName(structField.Name, structField.Tag)
		if keyName == ignoreField {
			return nil
		}

		switch structField.Type.Kind() {
		case reflect.String, reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
			reflect.Float32, reflect.Float64, reflect.Uint, reflect.Uint8, reflect.Uint16,
			reflect.Uint32, reflect.Uint64, reflect.Bool, reflect.Interface, reflect.Array,
			reflect.Slice:
			if fieldValue.CanSet() {
				err := m.setValue(fieldValue, getTagKey(tagName, keyName))
				if err != nil {
					return err
				}
			}
		case reflect.Ptr:
			err := m.handlePtr(fieldValue, getTagKey(tagName, keyName))
			if err != nil {
				return err
			}
		case reflect.Struct:
			err := m.handleStruct(fieldValue, getTagKey(tagName, keyName))
			if err != nil {
				return err
			}
		case reflect.Map:
			err := m.handleMap(rValue, fieldValue, getTagKey(tagName, keyName))
			if err != nil {
				return err
			}
		case reflect.Uintptr, reflect.Complex64, reflect.Complex128, reflect.Chan, reflect.Func,
			reflect.UnsafePointer:
			// ignore
		}
	}

	return nil
}

// handle map
func (m *Manager) handleMap(rValueForInline, rValue reflect.Value, tagName string) error {
	if tagName == doNotConsiderTag {
		if rValue.CanSet() {
			configValue := m.Configs()
			if configValue == nil {
				return nil
			}
			configRValue := reflect.ValueOf(configValue)
			rValue.Set(configRValue)
		}

		return nil
	}

	mapType := rValue.Type()
	// check if key is not string return error
	if mapType.Key().Kind() != reflect.String {
		return errors.New("map key should be string")
	}

	mapValue, err := m.populateMap(tagName, mapType, rValueForInline)
	if err != nil {
		return err
	}

	// if assignable then only assign
	if mapValue.Type() != mapType {
		return fmt.Errorf(fmtValueNotMatched,
			tagName, rValue.Kind(), mapValue.Kind())
	}

	if rValue.CanSet() {
		rValue.Set(mapValue)
	}

	return nil
}

func (m *Manager) getTagList(prefix string, rValues reflect.Value) []string {
	var tagList []string

	if strings.Contains(prefix, inline) {
		for i := 0; i < rValues.Type().NumField(); i++ {
			structField := rValues.Type().Field(i)
			if structField.Tag != `yaml:",inline"` {
				keyName := m.getKeyName(structField.Name, structField.Tag)
				tagList = append(tagList, keyName)
			}
		}
	}

	return tagList
}

func (m *Manager) getMapKeys(configValue map[string]interface{}, prefix string, tagList []string) ([]string,
	[]string, []string) {
	var (
		mapKeys, prefixForInline, inlineVal []string
	)

	if strings.Contains(prefix, inline) {
		pfx, iVal := checkPrefixForInline(prefix, tagList, configValue)

		if len(iVal) != 0 {
			inlineVal = iVal
		}

		if len(pfx) != 0 {
			prefixForInline = pfx
		}
	} else {
		for key := range configValue {
			isPrefix, index := checkPrefix(key, prefix+".")
			if !isPrefix || len(prefix) == 0 {
				continue
			}

			mapKeys = append(mapKeys, key[index-1:])
		}
	}

	return prefixForInline, inlineVal, mapKeys

}

func (m *Manager) setValuesForInline(mapValueType reflect.Type, inlineVal, prefixForInline []string, rValue reflect.Value) (reflect.Value, error) {
	mapValue := reflect.New(mapValueType)
	if len(inlineVal) != 0 {
		for _, iValues := range inlineVal {
			mapKey := iValues
			for _, pfx := range prefixForInline {
				if isSliceContainString(mapKey, strings.Split(pfx, ".")) {
					err := m.unmarshal(mapValue, getTagKey(pfx, doNotConsiderTag))
					if err != nil {
						return rValue, err
					}
					if rValue.CanSet() {
						rValue.SetMapIndex(reflect.ValueOf(mapKey), mapValue.Elem())
					}
				}
			}
		}
	}

	return rValue, nil
}

// generate map from config map
func (m *Manager) populateMap(prefix string, mapType reflect.Type, rValues reflect.Value) (reflect.Value, error) {
	tagList := m.getTagList(prefix, rValues)

	rValuePtr := reflect.New(mapType)
	rValue := rValuePtr.Elem()
	rValue.Set(reflect.MakeMap(mapType))
	//rValue := reflect.MakeMap(mapType)
	mapValueType := rValue.Type().Elem()

	configValue := m.Configs()

	prefixForInline, inlineVal, mapKeys := m.getMapKeys(configValue, prefix, tagList)

	if strings.Contains(prefix, inline) {
		return m.setValuesForInline(mapValueType, inlineVal, prefixForInline, rValue)
	}
	for _, key := range mapKeys {
		// if key itself has map value stored
		if key == "" {
			val := m.GetConfig(prefix)
			setVal := reflect.ValueOf(val)
			if mapType != setVal.Type() {
				return rValue, fmt.Errorf("invalid value for map %s", mapType.String())

			}
			if rValue.CanSet() {
				rValue.Set(setVal)
			}
			return rValue, nil
		}

		switch mapValueType.Kind() {
		// for '.' separated configurations
		case reflect.String, reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
			reflect.Float32, reflect.Float64, reflect.Uint, reflect.Uint8, reflect.Uint16,
			reflect.Uint32, reflect.Uint64, reflect.Bool, reflect.Interface:
			val := m.GetConfig(prefix + key)
			setVal := reflect.ValueOf(val)

			// maybe next map type
			if mapValueType != setVal.Type() {
				return rValue, nil

			}

			returnCongValue, err := m.toRvalueType(setVal.Interface(), reflect.New(mapValueType).Elem())
			if err != nil {
				return rValue, fmt.Errorf(fmtValueNotMatched,
					prefix+key, mapValueType, setVal.String())
			}

			if rValue.CanSet() {
				rValue.SetMapIndex(reflect.ValueOf(key[1:]), returnCongValue)
			}
		default:
			splitKey := strings.Split(key, `.`)
			mapKey := splitKey[1]
			mapValue := reflect.New(mapValueType)
			err := m.unmarshal(mapValue, getTagKey(prefix, mapKey))
			if err != nil {
				return rValue, err
			}

			if rValue.CanSet() {
				rValue.SetMapIndex(reflect.ValueOf(mapKey), mapValue.Elem())
			}
		}
	}

	return rValue, nil
}

func isSliceContainString(str string, list []string) bool {
	for _, value := range list {
		if value == str {
			return true
		}
	}
	return false
}

func getUniqueKeys(strSlice []string) []string {
	keys := make(map[string]bool)
	list := []string{}
	for _, val := range strSlice {
		if _, value := keys[val]; !value {
			keys[val] = true
			list = append(list, val)
		}
	}

	return list
}

func checkAndReplaceInline(prefix string, tagList []string, configValue map[string]interface{}) ([]string, []string) {
	var (
		inlineExist                                     bool
		updatedTagList, inlineVal, uniqueVal, inlinePfx []string
		indexPrefix                                     int
		heapData                                        string
	)

	firstValue := strings.Split(prefix, ".inline")
	// updatedTagList slice contains all the tags of the structure which has inline tag
	for _, value := range tagList {
		updatedTagList = append(updatedTagList, firstValue[0]+"."+value)
	}

	for heap := range configValue {
		// This condition is to get the index of inline tag so that it can be replace with the proper value
		splittedPrefix := strings.Split(prefix, ".")
		if len(splittedPrefix) != 0 {
			for i, j := range splittedPrefix {
				if j == inline {
					indexPrefix = i
				}
			}
		}

		splittedHeap := strings.Split(heap, ".")
		if len(splittedPrefix) != len(splittedHeap) {
			// checks all the word before inline tag should be equal
			// ex: if prefix is "cse.loadbalance.inline" and the heap is "cse.loadbalance.stratergy" then only we should consider
			for i := 0; i < indexPrefix; i++ {
				if splittedHeap[i] == splittedPrefix[i] {
					inlineExist = true
				} else {
					inlineExist = false
					break
				}
			}

			if inlineExist {
				for index, heapValue := range splittedHeap {
					if index > indexPrefix {
						break
					}

					heapData = heapData + "." + heapValue
					heapData = strings.TrimPrefix(heapData, ".")
				}

				if !isSliceContainString(heapData, updatedTagList) {
					inlineVal = append(inlineVal, splittedHeap[indexPrefix])
				}
			}
		}
	}

	inlineVal = getUniqueKeys(inlineVal)
	for _, v := range inlineVal {
		if !isSliceContainString(v, tagList) {
			uniqueVal = append(uniqueVal, v)
		}
	}

	for _, v := range uniqueVal {
		inlinePfx = append(inlinePfx, firstValue[0]+"."+v)
	}

	return inlinePfx, uniqueVal
}

func checkPrefixForInline(prefix string, tagList []string, configValue map[string]interface{}) ([]string, []string) {
	var inlineVal, pfxInline []string

	if strings.Contains(prefix, inline) {
		pfxInline, inlineVal = checkAndReplaceInline(prefix, tagList, configValue)
	}

	return pfxInline, inlineVal
}

func checkPrefix(heap, prefix string) (bool, int) {

	if len(heap) < len(prefix) {
		return false, 0
	}

	var index int
	for i := range prefix {
		if heap[i] != prefix[i] {
			break
		}
		index++
	}
	if len(prefix) != index {
		return false, 0
	}

	return true, index
}

// set values in object
func (m *Manager) setValue(rValue reflect.Value, keyName string) error {
	configValue := m.GetConfig(keyName)
	if configValue == nil {
		return nil
	}

	// assign value if assignable
	configRValue := reflect.ValueOf(configValue)

	returnCongValue, err := m.toRvalueType(configRValue.Interface(), rValue)
	if err != nil {
		return fmt.Errorf(fmtValueNotMatched,
			keyName, rValue.Kind(), configRValue.Kind())
	}

	if rValue.CanSet() {
		rValue.Set(returnCongValue)
	}

	return nil
}

// get key from tag
func (*Manager) getKeyName(fieldName string, fieldTagName reflect.StructTag) string {
	tagName := fieldTagName.Get(configClientTag)
	if tagName == "-" {
		return ignoreField
	} else if tagName == "" {
		return toSnake(fieldName)
	} else if tagName == ",inline" {
		tag := strings.Split(tagName, ",")
		tagName = tag[1]
		return tagName
	}

	return tagName
}

//convert camel case to snake case
func toSnake(in string) string {
	runes := []rune(in)
	length := len(runes)

	var out []rune
	for i := 0; i < length; i++ {
		if i > 0 && unicode.IsUpper(runes[i]) && ((i+1 < length && unicode.IsLower(runes[i+1])) ||
			unicode.IsLower(runes[i-1])) {
			out = append(out, '_')
		}
		out = append(out, unicode.ToLower(runes[i]))
	}

	return string(out)
}

// ToRvalueType Deserializes the object to a particular type
func (m *Manager) toRvalueType(confValue interface{}, rValue reflect.Value) (returnValue reflect.Value, err error) {
	convertType := rValue.Type()
	castValue := cast.NewValue(confValue, nil)
	returnValue = reflect.New(convertType).Elem()

	switch convertType.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		returnInt, rErr := castValue.ToInt64()
		if err != nil {
			err = rErr
		}
		returnValue.SetInt(returnInt)

	case reflect.String:
		returnString, rErr := castValue.ToString()
		if err != nil {
			err = rErr
		}

		returnValue.SetString(returnString)

	case reflect.Float32, reflect.Float64:
		returnFloat, rErr := castValue.ToFloat64()
		if err != nil {
			err = rErr
		}
		returnValue.SetFloat(returnFloat)

	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		returnUInt, rErr := castValue.ToUint64()
		if err != nil {
			err = rErr
		}
		returnValue.SetUint(returnUInt)
	case reflect.Bool:
		returnBool, rErr := castValue.ToBool()
		if err != nil {
			err = rErr
		}
		returnValue.SetBool(returnBool)

	case reflect.Array, reflect.Slice:
		return m.toArrayType(confValue, rValue)
	case reflect.Struct:
		return m.toStructType(confValue, rValue)
	case reflect.Ptr:
		return m.toPtrType(confValue, rValue)
	default:
		err = errors.New("can not convert type")
	}

	return returnValue, err
}

// toArrayType Deserializes the Array to a particular type
func (m *Manager) toArrayType(confValue interface{}, rValue reflect.Value) (returnValue reflect.Value, err error) {
	convertType := rValue.Type()
	returnValue = reflect.New(convertType).Elem()

	to, ok := confValue.([]interface{})
	if !ok {
		returnValue.Set(rValue)
		return returnValue, err
	}

	et := convertType.Elem()
	l := len(to)
	switch convertType.Kind() {
	case reflect.Slice:
		returnValue.Set(reflect.MakeSlice(convertType, l, l))
	case reflect.Array:
		if l != convertType.Len() {
			err = errors.New(fmt.Sprintf("invalid array: want %d elements but got %d", convertType.Len(), l))
		}
	}

	j := 0
	for i := 0; i < l; i++ {
		e := reflect.New(et).Elem()
		if r, err := m.toRvalueType(to[i], e); err == nil {
			returnValue.Index(j).Set(r)
			j++
		}
	}

	if convertType.Kind() != reflect.Array {
		returnValue.Set(returnValue.Slice(0, j))
	}

	return returnValue, err
}

// ToRvalueType Deserializes the Struct to a particular type
func (m *Manager) toStructType(confValue interface{}, rValue reflect.Value) (returnValue reflect.Value, err error) {
	structType := rValue.Type()
	returnValue = reflect.New(structType).Elem()
	numOfField := structType.NumField()

	for i := 0; i < numOfField; i++ {
		structField := structType.Field(i)
		fieldValue := rValue.Field(i)
		keyName := m.getKeyName(structField.Name, structField.Tag)
		if v, ok := confValue.(map[string]interface{}); ok {
			r, err := m.toRvalueType(v[keyName], fieldValue)
			if err == nil && fieldValue.CanSet() {
				fieldValue.Set(r)
			}
		}
	}
	returnValue.Set(rValue)

	return returnValue, err
}

// ToRvalueType Deserializes the Ptr to a particular type
func (m *Manager) toPtrType(confValue interface{}, rValue reflect.Value) (returnValue reflect.Value, err error) {
	convertType := rValue.Type()
	returnValue = reflect.New(convertType).Elem()

	if rValue.IsNil() {
		ptrValue := reflect.New(rValue.Type().Elem())
		_, err := m.toRvalueType(confValue, ptrValue)
		if err != nil {
			return returnValue, err
		}

		if rValue.CanSet() {
			rValue.Set(ptrValue)
			returnValue.Set(rValue)
		}

		return returnValue, err
	}

	if rValue.Elem().Kind() == reflect.Ptr {
		ptrValue := rValue.Elem()
		_, err := m.toRvalueType(confValue, ptrValue)
		if err != nil {
			return returnValue, err
		}
	}

	_, err = m.toRvalueType(confValue, rValue.Elem())
	return returnValue, err
}
