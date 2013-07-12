package xmlutil

import (
	"bufio"
	"bytes"
	"encoding/xml"
	"io"
	"reflect"
	"strconv"
	"time"
)

type UnsupportedTypeError struct {
	Type reflect.Type
}

func (typeError *UnsupportedTypeError) Error() string {
	return "xmlutil: unsupported type: " + typeError.Type.String()
}

func (x *XmlUtil) Marshal(v interface{}) ([]byte, error) {
	var b bytes.Buffer
	if err := x.NewEncoder(&b).Encode(v); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

type Encoder struct {
	xmlutil *XmlUtil
	writer  *bufio.Writer
}

func (x *XmlUtil) NewEncoder(w io.Writer) *Encoder {
	return &Encoder{x, bufio.NewWriter(w)}
}

func (e *Encoder) Encode(v interface{}) error {
	err := e.marshalValue(reflect.ValueOf(v), nil)
	e.writer.Flush()
	return err
}

func (e *Encoder) marshalValue(val reflect.Value, name *xml.Name) error {
	if !val.IsValid() {
		return nil
	}

	kind := val.Kind()
	typ := val.Type()

	if kind == reflect.Ptr || kind == reflect.Interface {
		if val.IsNil() {
			return nil
		}
		return e.marshalValue(val.Elem(), name)
	}

	if (kind == reflect.Slice || kind == reflect.Array) && typ.Elem().Kind() != reflect.Uint8 {
		for i, n := 0, val.Len(); i < n; i++ {
			err := e.marshalValue(val.Index(i), name)
			if err != nil {
				return err
			}
		}
		return nil
	}

	ti, err := e.xmlutil.getTypeInfo(typ)
	if err != nil {
		return err
	}

	if name == nil {
		name = &ti.name
	}
	tag := name.Local
	if name.Space != "" {
		if prefix := e.xmlutil.lookupPrefix(name.Space); prefix != "" {
			tag = prefix + ":" + tag
		}
	}

	e.writer.WriteByte('<')
	e.writer.WriteString(tag)
	err = e.marshalAttributes(val, ti)
	if err != nil {
		return nil
	}
	e.writer.WriteByte('>')

	if kind == reflect.Struct {
		err = e.marshalFields(val, ti)
	} else {
		err = e.marshalText(val, ti)
	}
	if err != nil {
		return err
	}

	e.writer.WriteByte('<')
	e.writer.WriteByte('/')
	e.writer.WriteString(tag)
	e.writer.WriteByte('>')

	return nil
}

func (e *Encoder) marshalAttributes(val reflect.Value, ti *typeInfo) error {
	check := make(map[xml.Name]bool) //string

	for _, fi := range ti.fields {
		if fi.flags&fAttr == 0 {
			continue
		}
		if check[fi.name] {
			continue
		}
		check[fi.name] = true
		var tag string
		if prefix := e.xmlutil.lookupPrefix(fi.name.Space); prefix != "" {
			tag = prefix + ":" + fi.name.Local
		} else {
			tag = fi.name.Local
		}
		e.writer.WriteByte(' ')
		e.writer.WriteString(tag)
		e.writer.WriteByte('=')
		e.writer.WriteByte('"')
		fval := val.Field(fi.index)
		err := e.marshalText(fval, ti)
		if err != nil {
			return err
		}
		e.writer.WriteByte('"')
	}

	for _, attr := range ti.attrs {
		if check[attr.Name] {
			continue
		}
		check[attr.Name] = true
		var tag string
		if prefix := e.xmlutil.lookupPrefix(attr.Name.Space); prefix != "" {
			tag = prefix + ":" + attr.Name.Local
		} else {
			tag = attr.Name.Local
		}
		e.writer.WriteByte(' ')
		e.writer.WriteString(tag)
		e.writer.WriteByte('=')
		e.writer.WriteByte('"')
		xml.Escape(e.writer, []byte(attr.Value))
		e.writer.WriteByte('"')
	}

	return nil
}

func (e *Encoder) marshalFields(val reflect.Value, ti *typeInfo) error {
	for _, fi := range ti.fields {
		if fi.flags&fAttr != 0 {
			continue
		}
		fval := val.Field(fi.index)
		name := fi.name

		if fi.flags&fOmitEmpty != 0 && isEmptyValue(fval) {
			continue
		}
		if fi.flags&fInterface != 0 {
			rti, err := e.xmlutil.getTypeInfo(fval.Elem().Type())
			if err != nil {
				return err
			}
			name = rti.name
		}
		err := e.marshalValue(fval, &name)
		if err != nil {
			return err
		}
	}
	return nil
}

var timeType = reflect.TypeOf(time.Time{})

func (e *Encoder) marshalText(val reflect.Value, ti *typeInfo) error {
	if val.Type() == timeType {
		e.writer.WriteString(val.Interface().(time.Time).Format(time.RFC3339Nano))
		return nil
	}
	switch val.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		e.writer.WriteString(strconv.FormatInt(val.Int(), 10))
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		e.writer.WriteString(strconv.FormatUint(val.Uint(), 10))
	case reflect.Float32, reflect.Float64:
		e.writer.WriteString(strconv.FormatFloat(val.Float(), 'g', -1, 64))
	case reflect.String:
		xml.Escape(e.writer, []byte(val.String()))
	case reflect.Bool:
		e.writer.WriteString(strconv.FormatBool(val.Bool()))
	case reflect.Array:
		// will be [...]byte
		bytes := make([]byte, val.Len())
		for i := range bytes {
			bytes[i] = val.Index(i).Interface().(byte)
		}
		xml.Escape(e.writer, bytes)
	case reflect.Slice:
		// will be []byte
		xml.Escape(e.writer, val.Bytes())
	default:
		return &UnsupportedTypeError{val.Type()}
	}
	return nil
}

func isEmptyValue(v reflect.Value) (empty bool) {
	switch v.Kind() {
	case reflect.Array, reflect.Map, reflect.Slice, reflect.String:
		empty = v.Len() == 0
	case reflect.Bool:
		empty = !v.Bool()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		empty = v.Int() == 0
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		empty = v.Uint() == 0
	case reflect.Float32, reflect.Float64:
		empty = v.Float() == 0
	case reflect.Interface, reflect.Ptr:
		empty = v.IsNil()
	}
	return
}
