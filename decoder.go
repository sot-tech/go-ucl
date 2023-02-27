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

package ucl

import (
	"errors"
	"fmt"
	"io"
)

// KeyOrder is order of the keys as they appear in the file; this allows the user to
// have their own order for items.
const KeyOrder = "--ucl-keyorder--"

var (
	errUnexpectedMultiline = errors.New("unexpected multi-line string")
	errUnexpectedBracket   = errors.New("unexpected \"{\", parent not nil|map|list")
	errKeyOrderNotSlice    = errors.New("map[--keyorder--] is not slice")
	errParentNotMap        = errors.New("parent is not map type")
	// ExportKeyOrder allows to disable constructing the KeyOrder arrays
	ExportKeyOrder = true
)

// Decoder holds internal state of parsed data
type Decoder struct {
	scanner *scanner

	ucl map[string]interface{}

	tags      []*tag
	tagsIndex int

	done bool
}

// NewDecoder creates new parser, which will read data from provided reader
func NewDecoder(r io.Reader) *Decoder {
	return &Decoder{
		scanner: newScanner(r),
		ucl:     make(map[string]interface{}),
	}
}

func (p *Decoder) nextTag() (*tag, error) {
	var err error

	if p.done {
		return nil, io.EOF
	}

	for {
		if p.tagsIndex >= len(p.tags) {
			p.tags, err = p.scanner.nextTags()
			if err != nil {
				return nil, err
			}
			p.tagsIndex = 0
		}
		for ; p.tagsIndex < len(p.tags); p.tagsIndex++ {
			m := p.tags[p.tagsIndex]
			if m.state == Whitespace || m.state == LComment ||
				m.state == HComment {
				continue
			}
			p.tagsIndex++

			return m, nil
		}
	}
}

func (p *Decoder) parseValue(t *tag, parent interface{}) (interface{}, error) {
	var err error

restart:
	if t == nil {
		t, err = p.nextTag()
		if err != nil {
			return nil, err
		}
	}

	switch t.state {
	case Tag, Quote, VQuote, Slash:
		// this could be either a value or a new key
		// have to see if the followon tags exist
		nt, err := p.nextTag()
		if err != nil {
			return nil, err
		}

		if nt == nil || nt.state == Semicolon || nt.state == Comma {
			return string(t.val), nil // leaf value; done
		}
		if nt.state == BraceClose || nt.state == BracketClose {
			nt.val = t.val
			return nt, nil
		}

		// "t" is a new key tag
		mapValue := make(map[string]interface{})
		res, err := p.parseValue(nt, parent)
		if err != nil {
			return nil, err
		}

		keyOrder := make([]string, 1)
		keyOrder[0] = string(t.val)
		mapValue[KeyOrder] = keyOrder
		mapValue[string(t.val)] = res
		return mapValue, nil

	case Semicolon:
		// no value, let parent handle it
		if parent == nil {
			return t, fmt.Errorf("unexpected ';' at line %d", p.scanner.line)
		}
		return parent, nil

	case Comma:
		// no value, let parent handle it
		if parent == nil {
			return t, fmt.Errorf("unexpected ',' at line %d", p.scanner.line)
		}
		return parent, nil

	case MlString:
		// this must only be a value
		return string(t.val), nil

	case BraceOpen:
		// {, new map
		return p.parse(t, parent)

	case BraceClose:
		// return until we hit the stack that has BraceOpen
		return parent, nil

	case BracketOpen:
		listValue := make([]interface{}, 0, 32)
		res, err := p.parseList(nil, listValue)
		return res, err

	case BracketClose:
		// list finished
		return parent, nil

	case Equal, Colon:
		t = nil
		goto restart
	}

	return nil, nil
}

func (p *Decoder) parseList(t *tag, parent []interface{}) (ret interface{}, err error) {
	// Parse until bracket close
restart:
	if t == nil {
		t, err = p.nextTag()
		if err != nil {
			return nil, err
		}
	}
	switch t.state {
	case BracketClose:
		// list finished
		return parent, nil
	case Semicolon, Colon, Equal:
		// no value, let parent handle it
		return nil, fmt.Errorf("invalid tag %s line %d",
			string(t.val), p.scanner.line)
	case Comma:
		t = nil
		goto restart
	default:
		// append child
		var res interface{}
		if res, err = p.parseValue(t, nil); err != nil {
			return nil, err
		}
		if resTag, ok := res.(*tag); ok {
			// result is a tag; parseValue didn't handle it
			if resTag.state == BracketClose {
				ret = append(parent, string(resTag.val))
			} else {
				err = fmt.Errorf("unexpected tag %s, line %d",
					string(resTag.val), p.scanner.line)
			}
			return
		}
		parent, t = append(parent, res), nil
		goto restart
	}
}

func (p *Decoder) parse(t *tag, parent interface{}) (ret interface{}, err error) {
restart:
	if t == nil {
		t, err = p.nextTag()
		if err != nil {
			return nil, err
		}
	}
	switch t.state {
	case Tag, Quote, VQuote, Slash:
		mapParent, ok := parent.(map[string]interface{})
		if !ok {
			return nil, errParentNotMap
		}

		keyOrderIntf, ok := mapParent[KeyOrder]
		var keyOrder []string
		if !ok {
			if ExportKeyOrder {
				// only initialize if requested
				keyOrder = make([]string, 0, 16)
			}
		} else if keyOrder, ok = keyOrderIntf.([]string); !ok {
			return nil, errKeyOrderNotSlice
		}

		res, err := p.parseValue(nil, nil)
		if err != nil {
			if resTag, ok := res.(*tag); ok {
				if resTag.state == Semicolon {
					// no value for key, make it == null
					res = nil
				}
			} else {
				return nil, err
			}
		} else if resTag, ok := res.(*tag); ok {
			// result is a tag; parseValue didn't handle it
			if resTag.state != BraceClose {
				t = resTag
				goto restart
			}
			res, t = string(resTag.val), resTag
		}
		// new key
		k := string(t.val)
		if mapItems, ok := mapParent[k]; ok {
			if childArray, ok := mapItems.([]interface{}); ok {
				// already an array, so append
				childArray = append(childArray, res)
				mapParent[k] = childArray
			} else {
				childArray = make([]interface{}, 1, 2)
				childArray[0] = mapParent[k]
				childArray = append(childArray, res)
				mapParent[k] = childArray
			}
		} else {
			// doesn't exist
			if cap(keyOrder) != 0 {
				// only update KeyOrder if it was initialized
				keyOrder = append(keyOrder, k)
				mapParent[KeyOrder] = keyOrder
			}
			mapParent[k] = res
		}
		if t.state == BraceClose {
			// map completed
			return parent, nil
		}
		// done for this tag; go to next
		t = nil
		goto restart
	case Semicolon:
		t = nil
		goto restart
	case MlString:
		// shouldn't happen
		err = errUnexpectedMultiline
	case BraceOpen:
		// If parent is not a map and not nil, then error
		var mapParent interface{}
		var ok bool
		if parent == nil {
			mapParent = make(map[string]interface{})
		} else if mapParent, ok = parent.(map[string]interface{}); !ok {
			if mapParent, ok = parent.([]interface{}); !ok {
				return nil, errUnexpectedBracket
			}
		}
		return p.parse(nil, mapParent)
	case BraceClose:
		// map finished
		ret = parent
	case BracketOpen:
		return p.parseList(nil, make([]interface{}, 0, 32))
	case BracketClose:
		// list finished
		ret = parent
	}

	return
}

// Decode starts parsing of provided reader and returns result map
func (p *Decoder) Decode() (map[string]interface{}, error) {
	_, err := p.parse(nil, p.ucl)
	if errors.Is(err, io.EOF) {
		err = nil
	}
	return p.ucl, err
}
