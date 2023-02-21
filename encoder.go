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
)

type encoder struct {
	w        io.Writer
	indenter string
	newline  string
	tag      string
	nilVal   string
}

// Encode v as UCL.
// indenter = string to use as indentation
// tag = if v has struct components, then use tag to search for the tag's key
// nilVal = (verbatim) string representing null value in output
func Encode(w io.Writer, v interface{}, indenter, tag, nilVal string) error {
	newline := ""
	if indenter != "" {
		newline = "\n"
	}

	e := &encoder{w, indenter, newline, tag, nilVal}
	return e.doEncode(reflect.ValueOf(v), parentMap, 0)
}

func (e *encoder) doEncode(v reflect.Value, parentType, indent int) error {
	var indents string
	for i := 0; i < indent; i++ {
		indents += e.indenter
	}

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
func encodeStr(s string) string {
	qs := strconv.Quote(s)
	for i := 1; i < len(qs)-1; i++ {
		if !((qs[i] >= 'A' && qs[i] <= 'Z') ||
			(qs[i] >= 'a' && qs[i] <= 'z') ||
			(qs[i] >= '0' && qs[i] <= '9')) {
			return qs
		}
	}
	return s
}

func (e *encoder) encodeMap(v reflect.Value, parentType, indent int) (err error) {
	var indents string
	for i := 0; i < indent; i++ {
		indents += e.indenter
	}

	// test if keyorder key exist
	mv := v.MapIndex(reflect.ValueOf(KeyOrder))
	if mv.Kind() != 0 {
		if kOrder, ok := mv.Interface().([]string); ok {
			for i := range kOrder {
				if i > 0 {
					fmt.Fprint(e.w, e.newline)
				}
				fmt.Fprint(e.w, indents, encodeStr(kOrder[i]))

				cv := v.MapIndex(reflect.ValueOf(kOrder[i]))
				if cv.Kind() == reflect.Ptr {
					cv = cv.Elem()
				}
				if cv.Kind() == reflect.Interface {
					cv = cv.Elem()
				}
				if cv.Kind() != reflect.Invalid {
					fmt.Fprint(e.w, " ")
				}

				switch cv.Kind() {
				case reflect.Slice, reflect.Array:
					err = e.doEncode(cv, parentMap, indent)
				case reflect.Map, reflect.Struct:
					fmt.Fprint(e.w, "{", e.newline)
					err = e.doEncode(cv, parentMap, indent+1)
					fmt.Fprint(e.w, indents, "}")
				default:
					err = e.doEncode(cv, parentMap, indent+1)
				}
				if err != nil {
					break
				}
				if parentType != parentArray {
					fmt.Fprint(e.w, ";")
				}
			}
			if err == nil && len(kOrder) > 0 {
				fmt.Fprint(e.w, e.newline)
			}
			return err
		}
	}
	keys := v.MapKeys()
	for i := range keys {
		if i > 0 {
			fmt.Fprint(e.w, e.newline)
		}
		fmt.Fprint(e.w, indents,
			encodeStr(keys[i].Interface().(string)))

		cv := v.MapIndex(keys[i])
		if cv.Kind() == reflect.Ptr {
			cv = cv.Elem()
		}
		if cv.Kind() == reflect.Interface {
			cv = cv.Elem()
		}
		if cv.Kind() != reflect.Invalid {
			fmt.Fprint(e.w, " ")
		}

		switch cv.Kind() {
		case reflect.Slice, reflect.Array:
			err = e.doEncode(cv, parentMap, indent)
		case reflect.Map, reflect.Struct:
			fmt.Fprint(e.w, "{", e.newline)
			err = e.doEncode(cv, parentMap, indent+1)
			fmt.Fprint(e.w, indents, "}")
		default:
			err = e.doEncode(cv, parentMap, indent+1)
		}
		if err != nil {
			break
		}
		if parentType != parentArray {
			fmt.Fprint(e.w, ";")
		}
	}
	if err == nil && len(keys) > 0 {
		fmt.Fprint(e.w, e.newline)
	}

	return err
}

func (e *encoder) encodeStruct(v reflect.Value, parentType, indent int) (err error) {
	var indents string
	for i := 0; i < indent; i++ {
		indents += e.indenter
	}

	nfields := v.Type().NumField()
	cnt := 0
	nonl := false
	for i := 0; i < nfields; i++ {
		if cnt > 0 && !nonl {
			fmt.Fprint(e.w, e.newline)
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
			err = e.encodeStruct(cv, parentAnon, indent)
			if err != nil {
				return err
			}
		}
		cnt++

		tag := sf.Tag.Get(e.tag)
		if tag == "-" {
			// skip
			continue
		}

		if tag == "" {
			if sf.Name[0] >= 'A' && sf.Name[0] <= 'Z' {
				fmt.Fprint(e.w, indents, sf.Name)
			} else {
				continue
			}
		} else {
			// split at "," and get first
			fmt.Fprint(e.w, indents,
				encodeStr(strings.SplitN(tag, ",", 2)[0]))
		}

		if cv.Kind() != reflect.Invalid {
			fmt.Fprint(e.w, " ")
		}

		switch cv.Kind() {
		case reflect.Slice, reflect.Array:
			err = e.doEncode(cv, parentMap, indent)
		case reflect.Map, reflect.Struct:
			fmt.Fprint(e.w, "{", e.newline)
			err = e.doEncode(cv, parentMap, indent+1)
			fmt.Fprint(e.w, indents, "}")
		default:
			err = e.doEncode(cv, parentMap, indent+1)
		}
		if err != nil {
			break
		}
		fmt.Fprint(e.w, ";")
	}
	if err == nil && nfields > 0 && parentType != parentArray &&
		parentType != parentAnon {
		fmt.Fprint(e.w, e.newline)
	}

	return err
}

func (e *encoder) encodeSlice(v reflect.Value, indent int) (err error) {
	var indents string
	for i := 0; i < indent; i++ {
		indents += e.indenter
	}

	fmt.Fprint(e.w, "[")
	for i := 0; i < v.Len(); i++ {
		if i == 0 {
			fmt.Fprint(e.w, e.newline)
		} else {
			fmt.Fprint(e.w, ",", e.newline)
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
			err = e.doEncode(cv, parentArray, indent)
		case reflect.Map, reflect.Struct:
			fmt.Fprint(e.w, e.indenter, indents, "{", e.newline)
			err = e.doEncode(cv, parentArray, indent+2)
			fmt.Fprint(e.w, e.indenter, indents, "}")
		default:
			err = e.doEncode(cv, parentArray, indent+1)
		}
		if err != nil {
			break
		}
	}
	if v.Len() > 0 {
		fmt.Fprintf(e.w, e.newline, indents)
	}
	fmt.Fprint(e.w, "]")
	return err
}

func (e *encoder) encodeScalar(v reflect.Value, parentType, indent int) (err error) {
	var indents string
	for i := 0; i < indent; i++ {
		indents += e.indenter
	}

	if parentType == parentArray {
		fmt.Fprint(e.w, indents)
	}

	if v.Kind() == reflect.Interface {
		v = v.Elem()
	}

	switch v.Kind() {
	case reflect.Bool:
		fmt.Fprint(e.w, v.Bool())
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
			fmt.Fprint(e.w, "<<EOSTR\n")
		} else if len(s) == 0 {
			fmt.Fprint(e.w, `""`)
			break
		} else if s[0] != '/' {
			fmt.Fprint(e.w, encodeStr(s))
			break
		}

		fmt.Fprint(e.w, s)
		if mlstring {
			fmt.Fprint(e.w, "\nEOSTR")
		}

	case reflect.Invalid:
		if e.nilVal != "" {
			fmt.Fprint(e.w, " ", e.nilVal)
		}

	default:
		fmt.Fprint(e.w, v.Interface())
	}
	return nil
}
