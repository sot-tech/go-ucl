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

type Decoder struct {
	scanner *scanner

	ucl map[string]interface{}

	tags      []*tag
	tagsIndex int

	done bool
}

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
			if m.state == WHITESPACE || m.state == LCOMMENT ||
				m.state == HCOMMENT {
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
	case TAG, QUOTE, VQUOTE, SLASH:
		// this could be either a value or a new key
		// have to see if the followon tags exist
		nt, err := p.nextTag()
		if err != nil {
			return nil, err
		}

		if nt == nil || nt.state == SEMICOL || nt.state == COMMA {
			return string(t.val), nil // leaf value; done
		}
		if nt.state == BRACECLOSE || nt.state == BRACKETCLOSE {
			nt.val = t.val
			return nt, nil
		}

		// "t" is a new key tag
		mapValue := make(map[string]interface{})
		res, err := p.parseValue(nt, parent)

		if err != nil {
			return nil, err
		}

		kOrder := make([]string, 1, 16)
		kOrder[0] = string(t.val)
		mapValue[KeyOrder] = kOrder
		mapValue[string(t.val)] = res
		return mapValue, nil

	case SEMICOL:
		// no value, let parent handle it
		if parent == nil {
			return t, fmt.Errorf("unexpected ';' at line %d", p.scanner.line)
		}
		return parent, nil

	case COMMA:
		// no value, let parent handle it
		if parent == nil {
			return t, fmt.Errorf("unexpected ',' at line %d", p.scanner.line)
		}
		return parent, nil

	case MLSTRING:
		// this must only be a value
		return string(t.val), nil

	case BRACEOPEN:
		// {, new map
		return p.parse(t, parent)

	case BRACECLOSE:
		// return until we hit the stack that has BRACEOPEN
		return parent, nil

	case BRACKETOPEN:
		listValue := make([]interface{}, 0, 32)
		res, err := p.parseList(nil, listValue)
		return res, err

	case BRACKETCLOSE:
		// list finished
		return parent, nil

	case EQUAL, COLON:
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
	case BRACKETCLOSE:
		// list finished
		return parent, nil

	case SEMICOL, COLON, EQUAL:
		// no value, let parent handle it
		return nil, fmt.Errorf("invalid tag %s line %d",
			string(t.val), p.scanner.line)
	case COMMA:
		t = nil
		goto restart

	default:
		// append child
		res, err := p.parseValue(t, nil)
		if err != nil {
			return nil, err
		} else {
			if resTag, ok := res.(*tag); ok {
				// result is a tag; parseValue didn't handle it
				if resTag.state == BRACKETCLOSE {
					parent = append(parent, string(resTag.val))
					return parent, nil
				} else {
					return nil, fmt.Errorf("Unexpected tag %s, line %d\n",
						string(resTag.val), p.scanner.line)
				}
			}

			parent = append(parent, res)
		}
		t = nil
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
	case TAG, QUOTE, VQUOTE, SLASH:

		mapParent, ok := parent.(map[string]interface{})
		if !ok {
			return nil, errParentNotMap
		}

		kOrderIntf, ok := mapParent[KeyOrder]
		var kOrder []string
		if !ok {
			if ExportKeyOrder {
				// only initialize if requested
				kOrder = make([]string, 0, 16)
			}
		} else {
			kOrder, ok = kOrderIntf.([]string)
			if !ok {
				return nil, errKeyOrderNotSlice
			}
		}

		res, err := p.parseValue(nil, nil)
		if err != nil {
			if resTag, ok := res.(*tag); ok {
				if resTag.state == SEMICOL {
					// no value for key, make it == null
					res = nil
				}
			} else {
				return nil, err
			}
		} else if resTag, ok := res.(*tag); ok {
			// result is a tag; parseValue didn't handle it
			if resTag.state != BRACECLOSE {
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
			if cap(kOrder) != 0 {
				// only update KeyOrder if it was initialized
				kOrder = append(kOrder, k)
				mapParent[KeyOrder] = kOrder
			}
			mapParent[k] = res
		}
		if t.state == BRACECLOSE {
			// map completed
			return parent, nil
		}

		// done for this tag; go to next
		t = nil
		goto restart

	case SEMICOL:
		t = nil
		goto restart

	case MLSTRING:
		// shouldn't happen
		return nil, errUnexpectedMultiline

	case BRACEOPEN:
		// {
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

	case BRACECLOSE:
		// map finished
		return parent, nil

	case BRACKETOPEN:
		return p.parseList(nil, make([]interface{}, 0, 32))

	case BRACKETCLOSE:
		// list finished
		return parent, nil
	}

	return nil, nil
}

func (p *Decoder) Decode() (map[string]interface{}, error) {
	_, err := p.parse(nil, p.ucl)
	if errors.Is(err, io.EOF) {
		err = nil
	}
	return p.ucl, err
}
