package xmlutil

import (
	"bytes"
	"encoding/xml"
	"errors"
	"io"
	"reflect"
	"strconv"
	"strings"
	"time"
)

type UnknownTypeError struct {
	Name xml.Name
}

func (typeError *UnknownTypeError) Error() (msg string) {
	if typeError.Name.Space != "" {
		msg = "xmlutil: unknown type with name: " + typeError.Name.Space + ":" + typeError.Name.Local
	} else {
		msg = "xmlutil: unknown type with name: " + typeError.Name.Local
	}
	return
}

func (x *XmlUtil) Unmarshal(data []byte, v interface{}) error {
	return x.NewDecoder(bytes.NewBuffer(data)).Decode(v)
}

type Decoder struct {
	xmlutil *XmlUtil
	parser  *xml.Decoder
}

func (x *XmlUtil) NewDecoder(r io.Reader) *Decoder {
	return &Decoder{x, xml.NewDecoder(r)}
}

func (d *Decoder) Decode(v interface{}) error {
	return d.DecodeElement(v, nil)
}

func (d *Decoder) DecodeElement(v interface{}, start *xml.StartElement) error {
	val := reflect.ValueOf(v)
	if val.Kind() != reflect.Ptr {
		return errors.New("xmlutil: non-pointer passed to Unmarshal")
	}
	elem := val.Elem()
	if elem.Kind() == reflect.Slice {
		err := d.unmarshal(elem, start)
		for {
			err = d.unmarshal(elem, nil)
			if err != nil {
				break
			}
		}
		if err == io.EOF {
			return nil
		}
		return err
	}
	return d.unmarshal(elem, start)
}

func (d *Decoder) Find(names []xml.Name) (*xml.StartElement, error) {
	for {
		tok, err := d.parser.Token()
		if err != nil {
			return nil, err
		}
		if start, ok := tok.(xml.StartElement); ok {
			for _, name := range names {
				if start.Name == name {
					return &start, nil
				}
			}
		}
	}
}

func (d *Decoder) unmarshal(val reflect.Value, start *xml.StartElement) error {
	if start == nil {
		for {
			tok, err := d.parser.Token()
			if err != nil {
				return err
			}
			if t, ok := tok.(xml.StartElement); ok {
				start = &t
				break
			}
		}
	}

	switch val.Kind() {
	default:
		var data []byte
		depth := 0
	Loop:
		for {
			tok, err := d.parser.Token()
			if err != nil {
				return err
			}
			switch t := tok.(type) {
			case xml.StartElement:
				depth++
			case xml.CharData:
				// Shallow copy value, ignore nested tags
				if depth == 0 {
					data = append(data, t...)
				}
			case xml.EndElement:
				if depth == 0 {
					break Loop
				}
				depth--
			}
		}
		copyValue(val, data)
	case reflect.Struct:
		err := d.unmarshalFields(val, start)
		if err != nil {
			return err
		}
	case reflect.Slice:
		n := val.Len()
		if n >= val.Cap() {
			ncap := 2 * n
			if ncap < 4 {
				ncap = 4
			}
			nval := reflect.MakeSlice(val.Type(), n, ncap)
			reflect.Copy(nval, val)
			val.Set(nval)
		}
		val.SetLen(n + 1)
		err := d.unmarshal(val.Index(n), start)
		if err != nil {
			val.SetLen(n)
			return err
		}
	case reflect.Interface:
		println(start.Name.Local)
		ntyp := d.xmlutil.getTypeByName(start.Name)
		if ntyp == nil {
			break
		}
		nval := reflect.New(ntyp).Elem()
		err := d.unmarshal(nval, start)
		if err != nil {
			return err
		}
		val.Set(nval)
	case reflect.Ptr:
		if val.IsNil() {
			val.Set(reflect.New(val.Type().Elem()))
		}
		err := d.unmarshal(val.Elem(), start)
		if err != nil {
			return err
		}
	}
	return nil
}

func (d *Decoder) unmarshalFields(val reflect.Value, start *xml.StartElement) error {
	ti, err := d.xmlutil.getTypeInfo(val.Type())

	if err != nil {
		return err
	}
	for _, attr := range start.Attr {
		for _, fi := range ti.fields {
			if fi.flags&fAttr == 0 {
				continue
			}
			if fi.name == attr.Name {
				fval := val.Field(fi.index)
				err := copyValue(fval, []byte(attr.Value))
				if err != nil {
					return err
				}
				break
			}
		}
	}

Loop:
	for {
		tok, err := d.parser.Token()
		if err != nil {
			return err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			// Fix when document doesn't declare namespace
			uri := d.xmlutil.lookupNamespaceUri(t.Name.Space)
			if uri != "" {
				t.Name.Space = uri
			}
			// Find a match in field for start tag
			for _, fi := range ti.fields {
				if fi.flags&fAttr != 0 {
					continue
				}
				if fi.name == t.Name || fi.flags&fInterface != 0 {
					fval := val.Field(fi.index)
					if !fval.IsValid() {
						continue Loop
					}
					err := d.unmarshal(fval, &t)
					if err != nil {
						return err
					}
					continue Loop
				}
			}
			// Couldn't find match, so eat the rest and continue
			depth := 0
			for {
				tok, err := d.parser.Token()
				if err != nil {
					return err
				}
				switch tok.(type) {
				case xml.StartElement:
					depth++
				case xml.EndElement:
					if depth == 0 {
						continue Loop
					}
					depth--
				}
			}
		case xml.EndElement:
			break Loop
		}
	}
	return nil
}

func copyValue(dst reflect.Value, src []byte) error {
	switch t := dst; t.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		value, err := strconv.ParseInt(string(src), 10, 64)
		if err != nil {
			return err
		}
		t.SetInt(value)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		value, err := strconv.ParseUint(string(src), 10, 64)
		if err != nil {
			return err
		}
		t.SetUint(value)
	case reflect.Float32, reflect.Float64:
		value, err := strconv.ParseFloat(string(src), 64)
		if err != nil {
			return err
		}
		t.SetFloat(value)
	case reflect.Bool:
		value, err := strconv.ParseBool(strings.TrimSpace(string(src)))
		if err != nil {
			return err
		}
		t.SetBool(value)
	case reflect.String:
		t.SetString(string(src))
	case reflect.Slice:
		if len(src) == 0 {
			src = []byte{}
		}
		t.SetBytes(src)
	case reflect.Struct:
		if t.Type() == timeType {
			tv, err := time.Parse(time.RFC3339, string(src))
			if err != nil {
				return err
			}
			t.Set(reflect.ValueOf(tv))
		}
	}
	return nil
}
