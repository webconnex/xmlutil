package xmlutil

import (
	"encoding/xml"
	"io"
	"reflect"
	"strings"
	"sync"
)

type XmlUtil interface {
	RegisterType(value interface{})
	RegisterTypeMore(value interface{}, name xml.Name, attrs []xml.Attr)
	RegisterNamespace(prefix, uri string)
	Marshal(v interface{}) ([]byte, error)
	Unmarshal(data []byte, v interface{}) error
	NewEncoder(w io.Writer) *Encoder
	NewDecoder(r io.Reader) *Decoder
}

type xmlutil struct {
	typeMap     map[reflect.Type]*typeInfo
	typeLock    sync.RWMutex
	nsPrefixMap map[string]string
	nsUriMap    map[string]string
	nsLock      sync.RWMutex
}

type typeInfo struct {
	name   xml.Name
	attrs  []xml.Attr
	fields []fieldInfo
}

type fieldFlags int

type fieldInfo struct {
	index int
	name  xml.Name
	flags fieldFlags
}

const (
	fElement fieldFlags = 1 << iota
	fAttr
	fInterface
	fOmitEmpty
)

func NewXmlUtil() XmlUtil {
	return &xmlutil{
		typeMap:     make(map[reflect.Type]*typeInfo),
		nsPrefixMap: map[string]string{"xmlns": "xmlns"},
		nsUriMap:    map[string]string{"xmlns": "xmlns"},
	}
}

func (x *xmlutil) RegisterType(value interface{}) {
	x.RegisterTypeMore(value, xml.Name{}, nil)
}

func (x *xmlutil) RegisterTypeMore(value interface{}, name xml.Name, attrs []xml.Attr) {
	typ := reflect.TypeOf(value)
	x.typeLock.RLock()
	_, ok := x.typeMap[typ]
	x.typeLock.RUnlock()
	if ok {
		panic("xmlutil: " + typ.Name() + " already registered")
	}
	x.registerType(typ, name, attrs)
}

func (x *xmlutil) registerType(typ reflect.Type, name xml.Name, attrs []xml.Attr) (*typeInfo, error) {
	if name.Local == "" {
		name.Local = typ.Name()
	}
	ti := &typeInfo{name: name, attrs: attrs}

	if typ.Kind() == reflect.Struct {
		ti.fields = x.getFields(typ)
	}

	x.typeLock.Lock()
	x.typeMap[typ] = ti
	x.typeLock.Unlock()
	return ti, nil
}

func (x *xmlutil) getTypeInfo(typ reflect.Type) (*typeInfo, error) {
	x.typeLock.RLock()
	ti, ok := x.typeMap[typ]
	x.typeLock.RUnlock()
	if ok {
		return ti, nil
	}
	return x.registerType(typ, xml.Name{}, nil)
}

func (x *xmlutil) getTypeByName(name xml.Name) reflect.Type {
	x.typeLock.RLock()
	defer x.typeLock.RUnlock()
	for typ, ti := range x.typeMap {
		if ti.name == name {
			return typ
		}
	}
	return nil
}

func (x *xmlutil) getFields(typ reflect.Type) []fieldInfo {
	n := typ.NumField()
	fields := make([]fieldInfo, 0, n)
	for i := 0; i < n; i++ {
		f := typ.Field(i)
		if f.PkgPath != "" {
			continue
		}
		if f.Anonymous {
			if c := f.Name[0]; c > 'Z' {
				continue
			}
		}

		fi := fieldInfo{index: i}
		tokens := strings.Split(f.Tag.Get("xml"), ",")
		tag := tokens[0]

		if i := strings.Index(tag, ":"); i >= 0 {
			fi.name.Space, fi.name.Local = x.lookupNamespaceUri(tag[:i]), tag[i+1:]
		} else {
			fi.name.Local = tag
		}
		if fi.name.Local == "" {
			fi.name.Local = f.Name
		}
		for _, flag := range tokens[1:] {
			switch flag {
			case "attr":
				fi.flags |= fAttr
			case "omitempty":
				fi.flags |= fOmitEmpty
			}
		}
		typ := f.Type
		if typ.Kind() == reflect.Slice {
			typ = typ.Elem()
		}
		if typ.Kind() == reflect.Interface {
			fi.flags |= fInterface
		}
		fields = append(fields, fi)
	}
	return fields
}

func (x *xmlutil) RegisterNamespace(uri, prefix string) {
	x.nsLock.Lock()
	x.nsPrefixMap[uri] = prefix
	x.nsUriMap[prefix] = uri
	x.nsLock.Unlock()
}

func (x *xmlutil) lookupPrefix(uri string) string {
	x.nsLock.RLock()
	defer x.nsLock.RUnlock()
	return x.nsPrefixMap[uri]
}

func (x *xmlutil) lookupNamespaceUri(prefix string) string {
	x.nsLock.RLock()
	defer x.nsLock.RUnlock()
	return x.nsUriMap[prefix]
}
