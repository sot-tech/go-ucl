/*
 * Copyright (c) 2015 Leon Dang, Nahanni Systems Inc
 * All rights reserved.
 *
 * Redistribution and use in source and binary forms, with or without
 * modification, are permitted provided that the following conditions
 * are met:
 *
 * 1. Redistributions of source code must retain the above copyright
 *    notice, this list of conditions and the following disclaimer
 *    in this position and unchanged.
 * 2. Redistributions in binary form must reproduce the above copyright
 *    notice, this list of conditions and the following disclaimer in the
 *    documentation and/or other materials provided with the distribution.
 *
 * THIS SOFTWARE IS PROVIDED BY THE AUTHOR AND CONTRIBUTORS "AS IS" AND
 * ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT LIMITED TO, THE
 * IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR A PARTICULAR PURPOSE
 * ARE DISCLAIMED. IN NO EVENT SHALL THE AUTHOR OR CONTRIBUTORS BE LIABLE
 * FOR ANY DIRECT, INDIRECT, INCIDENTAL, SPECIAL, EXEMPLARY, OR CONSEQUENTIAL
 * DAMAGES (INCLUDING, BUT NOT LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS
 * OR SERVICES; LOSS OF USE, DATA, OR PROFITS; OR BUSINESS INTERRUPTION)
 * HOWEVER CAUSED AND ON ANY THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT
 * LIABILITY, OR TORT (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY
 * OUT OF THE USE OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF
 * SUCH DAMAGE.
 */

// Package ucl encodes an interface into UCL format
package ucl

import (
	"bytes"
	"fmt"
	"io"
	"reflect"
	"strconv"
	"strings"
)

const (
	parentMap = iota
	parentArray
	parentAnon
	DefaultTag           = "ucl"
	DefaultLineSeparator = "\n"
	DefaultNilValue      = "nil"
	DefaultIndent        = "\t"
)

var (
	baSpace      = []byte{' '}
	baCBracketOp = []byte{'{'}
	baCBracketCl = []byte{'}'}
	baSBracketOp = []byte{'['}
	baSBracketCl = []byte{']'}
	baSemicolon  = []byte{';'}
	baComma      = []byte{','}
	baQuotes     = []byte{'"', '"'}
	baTrue       = []byte{'t', 'r', 'u', 'e'}
	baFalse      = []byte{'f', 'a', 'l', 's', 'e'}
	baSOL        = []byte("<<EOSTR\n")
	baEOL        = []byte("\nEOSTR")
)

type Encoder struct {
	w       io.Writer
	indent  []byte
	lineSep []byte
	tag     string
	nilVal  []byte
}

// Encode encodes v to byte array
func Encode(v interface{}) (out []byte, err error) {
	bb := new(bytes.Buffer)
	if err = NewEncoder(bb).Encode(v); err == nil {
		out = bb.Bytes()
	}
	return
}

// NewEncoder creates new UCL encoder, which writes output into w
func NewEncoder(w io.Writer) *Encoder {
	return &Encoder{
		w:       w,
		indent:  []byte(DefaultIndent),
		lineSep: []byte(DefaultLineSeparator),
		tag:     DefaultTag,
		nilVal:  []byte(DefaultNilValue),
	}
}

// SetIndent declares string to use as indentation (see DefaultIndent)
func (e *Encoder) SetIndent(indent string) {
	e.indent = []byte(indent)
}

// SetLineSeparator declares line separator (see DefaultLineSeparator)
func (e *Encoder) SetLineSeparator(nl string) {
	e.lineSep = []byte(nl)
}

// SetTag use provided tag to search for the tag's key
// when encoding struct
func (e *Encoder) SetTag(tag string) {
	e.tag = tag
}

// SetNilValue set (verbatim) string representing null value in output
func (e *Encoder) SetNilValue(nv string) {
	e.nilVal = []byte(nv)
}

// Encode v as UCL
func (e *Encoder) Encode(v interface{}) error {
	return e.doEncode(reflect.ValueOf(v), parentMap, 0)
}

func (e *Encoder) doEncode(v reflect.Value, parentType, indent int) error {
	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}
	if v.Kind() == reflect.Interface {
		v = v.Elem()
	}

	switch v.Kind() {
	case reflect.Map:
		if v.Type().Key().Kind() != reflect.String {
			return fmt.Errorf("<map> %v %s", v, "does not use string key")
		}
		return e.encodeMap(v, parentType, indent)
	case reflect.Struct:
		return e.encodeStruct(v, parentType, indent)
	case reflect.Slice, reflect.Array:
		return e.encodeSlice(v, indent)
	default:
		return e.encodeScalar(v, parentType, indent)
	}
}

// quote all strings that have non-alphanum
func encodeStr(s string) []byte {
	qs := strconv.Quote(s)
	for i := 1; i < len(qs)-1; i++ {
		if !((qs[i] >= 'A' && qs[i] <= 'Z') ||
			(qs[i] >= 'a' && qs[i] <= 'z') ||
			(qs[i] >= '0' && qs[i] <= '9')) {
			return []byte(qs)
		}
	}
	return []byte(s)
}

func mkIndent(count int, indent []byte) []byte {
	indents := make([]byte, 0, len(indent)*count)
	if len(indent) > 0 {
		for i := 0; i < count; i++ {
			indents = append(indents, indent...)
		}
	}
	return indents
}

func (e *Encoder) write(data ...[]byte) (err error) {
	switch len(data) {
	case 0:
	case 1:
		if len(data[0]) > 0 {
			_, err = e.w.Write(data[0])
		}
	default:
		dataLen := 0
		for _, d := range data {
			dataLen += len(d)
		}
		if dataLen > 0 {
			out := make([]byte, 0, dataLen)
			for _, d := range data {
				out = append(out, d...)
			}
			_, err = e.w.Write(out)
		}
	}
	return
}

func (e *Encoder) encodeMap(v reflect.Value, parentType, indent int) (err error) {
	indents := mkIndent(indent, e.indent)

	// test if keyorder key exist
	mv := v.MapIndex(reflect.ValueOf(KeyOrder))
	if mv.Kind() != reflect.Invalid {
		if kOrder, ok := mv.Interface().([]string); ok {
			for i := range kOrder {
				if i > 0 {
					if err = e.write(e.lineSep); err != nil {
						return
					}
				}
				if err = e.write(indents, encodeStr(kOrder[i])); err != nil {
					return
				}

				cv := v.MapIndex(reflect.ValueOf(kOrder[i]))
				if cv.Kind() == reflect.Ptr {
					cv = cv.Elem()
				}
				if cv.Kind() == reflect.Interface {
					cv = cv.Elem()
				}
				if cv.Kind() != reflect.Invalid {
					if err = e.write(baSpace); err != nil {
						return
					}
				}

				switch cv.Kind() {
				case reflect.Slice, reflect.Array:
					err = e.doEncode(cv, parentMap, indent)
				case reflect.Map, reflect.Struct:
					if err = e.write(baCBracketOp, e.lineSep); err != nil {
						return
					}
					if err = e.doEncode(cv, parentMap, indent+1); err != nil {
						return
					}
					if err = e.write(indents, baCBracketCl); err != nil {
						return
					}
				default:
					if err = e.doEncode(cv, parentMap, indent+1); err != nil {
						return
					}
				}
				if parentType != parentArray {
					if err = e.write(baSemicolon); err != nil {
						return
					}
				}
			}
			if len(kOrder) > 0 {
				err = e.write(e.lineSep)
			}
			return
		}
	}
	keys := v.MapKeys()
	for i := range keys {
		if i > 0 {
			if err = e.write(e.lineSep); err != nil {
				return
			}
		}
		if err = e.write(indents, encodeStr(keys[i].Interface().(string))); err != nil {
			return
		}

		cv := v.MapIndex(keys[i])
		if cv.Kind() == reflect.Ptr {
			cv = cv.Elem()
		}
		if cv.Kind() == reflect.Interface {
			cv = cv.Elem()
		}
		if cv.Kind() != reflect.Invalid {
			if err = e.write(baSpace); err != nil {
				return
			}
		}

		switch cv.Kind() {
		case reflect.Slice, reflect.Array:
			if err = e.doEncode(cv, parentMap, indent); err != nil {
				return
			}
		case reflect.Map, reflect.Struct:
			if err = e.write(baCBracketOp, e.lineSep); err != nil {
				return
			}
			if err = e.doEncode(cv, parentMap, indent+1); err != nil {
				return
			}
			if err = e.write(indents, baCBracketCl); err != nil {
				return
			}
		default:
			if err = e.doEncode(cv, parentMap, indent+1); err != nil {
				return
			}
		}
		if parentType != parentArray {
			if err = e.write(baSemicolon); err != nil {
				return
			}
		}
	}
	if len(keys) > 0 {
		err = e.write(e.lineSep)
	}

	return
}

func (e *Encoder) encodeStruct(v reflect.Value, parentType, indent int) (err error) {
	indents := mkIndent(indent, e.indent)

	nfields := v.Type().NumField()
	cnt := 0
	nonl := false
	for i := 0; i < nfields; i++ {
		if cnt > 0 && !nonl {
			if err = e.write(e.lineSep); err != nil {
				return
			}
		}
		nonl = false

		cv := v.Field(i)
		sf := v.Type().Field(i)

		if cv.Kind() == reflect.Ptr {
			cv = cv.Elem()
		}
		if cv.Kind() == reflect.Interface {
			cv = cv.Elem()
		}
		if sf.Anonymous {
			if cv.Kind() == reflect.Invalid {
				nonl = true
				continue
			}

			// Drill down into anonymous field and attempt encoding of it
			if err = e.encodeStruct(cv, parentAnon, indent); err != nil {
				return
			}
		}
		cnt++

		tag := sf.Tag.Get(e.tag)
		if tag == "-" {
			// skip
			continue
		}

		if tag == "" {
			nameRunes := []rune(sf.Name)
			if nameRunes[0] >= 'A' && nameRunes[0] <= 'Z' {
				if err = e.write(indents, []byte(sf.Name)); err != nil {
					return
				}
			} else {
				continue
			}
		} else {
			// split at "," and get first
			if err = e.write(indents, encodeStr(strings.SplitN(tag, ",", 2)[0])); err != nil {
				return
			}
		}

		if cv.Kind() != reflect.Invalid {
			if err = e.write(baSpace); err != nil {
				return
			}
		}

		switch cv.Kind() {
		case reflect.Slice, reflect.Array:
			if err = e.doEncode(cv, parentMap, indent); err != nil {
				return
			}
		case reflect.Map, reflect.Struct:
			if err = e.write(baCBracketOp, e.lineSep); err != nil {
				return
			}
			if err = e.doEncode(cv, parentMap, indent+1); err != nil {
				return
			}
			if err = e.write(indents, baCBracketCl); err != nil {
				return
			}
		default:
			if err = e.doEncode(cv, parentMap, indent+1); err != nil {
				return
			}
		}
		if err = e.write(baSemicolon); err != nil {
			return
		}
	}
	if nfields > 0 && parentType != parentArray && parentType != parentAnon {
		err = e.write(e.lineSep)
	}

	return
}

func (e *Encoder) encodeSlice(v reflect.Value, indent int) (err error) {
	indents := mkIndent(indent, e.indent)

	if err = e.write(baSBracketOp); err != nil {
		return
	}
	for i := 0; i < v.Len(); i++ {
		if i > 0 {
			if err = e.write(baComma); err != nil {
				return
			}
		}
		if err = e.write(e.lineSep); err != nil {
			return
		}

		cv := v.Index(i)
		if cv.Kind() == reflect.Ptr {
			cv = cv.Elem()
		}
		if cv.Kind() == reflect.Interface {
			cv = cv.Elem()
		}

		switch cv.Kind() {
		case reflect.Slice, reflect.Array:
			if err = e.doEncode(cv, parentArray, indent); err != nil {
				return
			}
		case reflect.Map, reflect.Struct:
			if err = e.write(e.indent, indents, baCBracketOp, e.lineSep); err != nil {
				return
			}
			if err = e.doEncode(cv, parentArray, indent+2); err != nil {
				return
			}
			if err = e.write(e.indent, indents, baCBracketCl); err != nil {
				return
			}
		default:
			if err = e.doEncode(cv, parentArray, indent+1); err != nil {
				return
			}
		}
	}
	if v.Len() > 0 {
		if err = e.write(e.lineSep, indents); err != nil {
			return
		}
	}
	err = e.write(baSBracketCl)
	return
}

func (e *Encoder) encodeScalar(v reflect.Value, parentType, indent int) (err error) {
	indents := mkIndent(indent, e.indent)

	if parentType == parentArray {
		if err = e.write(indents); err != nil {
			return
		}
	}

	if v.Kind() == reflect.Interface {
		v = v.Elem()
	}

	switch v.Kind() {
	case reflect.Bool:
		if v.Bool() {
			err = e.write(baTrue)
		} else {
			err = e.write(baFalse)
		}
	case reflect.String:
		mlstring := false
		s := v.String()
		nl := 0
		// push as multiline string if there are more than 3 newlines and
		// string is longer than 160 characters
		if len(s) > 160 {
			for i := range s {
				if s[i] == '\n' {
					nl++
					if nl > 3 {
						break
					}
				}
			}
		}
		if nl > 3 {
			mlstring = true
			if err = e.write(baSOL); err != nil {
				break
			}
		} else if len(s) == 0 {
			err = e.write(baQuotes)
			break
		} else if s[0] != '/' {
			err = e.write(encodeStr(s))
			break
		}

		if err = e.write([]byte(s)); err != nil {
			break
		}
		if mlstring {
			err = e.write(baEOL)
		}

	case reflect.Invalid:
		if len(e.nilVal) > 0 {
			err = e.write(baSpace, e.nilVal)
		}

	default:
		_, err = fmt.Fprint(e.w, v.Interface())
	}
	return
}
