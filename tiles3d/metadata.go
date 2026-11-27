// Copyright (c) 2026 Neomantra Corp.

package nt3dtiles3d

import (
	"encoding/json"
	"fmt"
)

// Schema describes the metadata a tileset carries: classes of entity, each
// with named properties, and enumerations a property may draw from.
type Schema struct {
	ID, Name, Description, Version string
	Classes                        map[string]Class
	Enums                          map[string]Enum
}

// Class is one kind of entity.
type Class struct {
	Name, Description string
	Properties        map[string]Property
}

// Property describes one property of a class. Type is SCALAR, VEC2, VEC3,
// VEC4, MAT2, MAT3, MAT4, STRING, BOOLEAN, or ENUM, and ComponentType the
// number type of a numeric one, such as FLOAT64.
type Property struct {
	Name, Description   string
	Type, ComponentType string
	// EnumType names the Schema's enumeration, for an ENUM.
	EnumType string
	// Array is a property holding a list, of Count values when Count is
	// not zero.
	Array bool
	Count int
	// Normalized, Offset, Scale, Min, Max, NoData, and Default are as the
	// specification defines them; the JSON values are as written.
	Normalized                               bool
	Offset, Scale, Min, Max, NoData, Default any
	Required                                 bool
	// Semantic says what the property means, such as TILE_GEOMETRIC_ERROR.
	Semantic string
}

// Enum is a named set of values.
type Enum struct {
	Name, Description string
	// ValueType is the integer type the values are stored as; UINT16 when
	// the schema does not say.
	ValueType string
	Values    []EnumValue
}

// EnumValue is one name of an enumeration.
type EnumValue struct {
	Name, Description string
	Value             int64
}

// Entity is metadata on a tileset, a tile, a group, a content, or a
// subtree: the class it is an instance of, and its properties as the JSON
// gives them (float64, string, bool, []any, or map[string]any), keyed by
// property name.
type Entity struct {
	Class      string
	Properties map[string]any
}

type rawEntity struct {
	Class      string         `json:"class"`
	Properties map[string]any `json:"properties"`
}

func (r *rawEntity) entity() *Entity {
	if r == nil {
		return nil
	}
	return &Entity{Class: r.Class, Properties: r.Properties}
}

type rawSchema struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Version     string `json:"version"`
	Classes     map[string]struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Properties  map[string]struct {
			Name          string `json:"name"`
			Description   string `json:"description"`
			Type          string `json:"type"`
			ComponentType string `json:"componentType"`
			EnumType      string `json:"enumType"`
			Array         bool   `json:"array"`
			Count         int    `json:"count"`
			Normalized    bool   `json:"normalized"`
			Offset        any    `json:"offset"`
			Scale         any    `json:"scale"`
			Min           any    `json:"min"`
			Max           any    `json:"max"`
			NoData        any    `json:"noData"`
			Default       any    `json:"default"`
			Required      bool   `json:"required"`
			Semantic      string `json:"semantic"`
		} `json:"properties"`
	} `json:"classes"`
	Enums map[string]struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		ValueType   string `json:"valueType"`
		Values      []struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			Value       int64  `json:"value"`
		} `json:"values"`
	} `json:"enums"`
}

var propertyTypes = map[string]bool{"SCALAR": true, "VEC2": true, "VEC3": true, "VEC4": true, "MAT2": true, "MAT3": true, "MAT4": true, "STRING": true, "BOOLEAN": true, "ENUM": true}

func (r *rawSchema) schema() (*Schema, error) {
	s := &Schema{ID: printable(r.ID, 256), Name: printable(r.Name, 256), Description: printable(r.Description, 1024), Version: printable(r.Version, 64)}
	if len(r.Classes) > 0 {
		s.Classes = make(map[string]Class, len(r.Classes))
	}
	for id, c := range r.Classes {
		class := Class{Name: printable(c.Name, 256), Description: printable(c.Description, 1024)}
		if len(c.Properties) > 0 {
			class.Properties = make(map[string]Property, len(c.Properties))
		}
		for name, p := range c.Properties {
			if !propertyTypes[p.Type] {
				return nil, fmt.Errorf("nt3dtiles3d: schema property %q has unknown type %q", printable(name, 64), printable(p.Type, 16))
			}
			if p.Count < 0 {
				return nil, fmt.Errorf("nt3dtiles3d: schema property %q has a negative count", printable(name, 64))
			}
			if p.Type == "ENUM" {
				if _, ok := r.Enums[p.EnumType]; !ok {
					return nil, fmt.Errorf("nt3dtiles3d: schema property %q names enum %q, which the schema lacks", printable(name, 64), printable(p.EnumType, 64))
				}
			}
			class.Properties[name] = Property{
				Name: printable(p.Name, 256), Description: printable(p.Description, 1024),
				Type: p.Type, ComponentType: printable(p.ComponentType, 16), EnumType: p.EnumType,
				Array: p.Array, Count: p.Count, Normalized: p.Normalized,
				Offset: p.Offset, Scale: p.Scale, Min: p.Min, Max: p.Max, NoData: p.NoData, Default: p.Default,
				Required: p.Required, Semantic: printable(p.Semantic, 64),
			}
		}
		s.Classes[id] = class
	}
	if len(r.Enums) > 0 {
		s.Enums = make(map[string]Enum, len(r.Enums))
	}
	for id, e := range r.Enums {
		enum := Enum{Name: printable(e.Name, 256), Description: printable(e.Description, 1024), ValueType: printable(e.ValueType, 16)}
		if enum.ValueType == "" {
			enum.ValueType = "UINT16"
		}
		for _, v := range e.Values {
			enum.Values = append(enum.Values, EnumValue{Name: printable(v.Name, 256), Description: printable(v.Description, 1024), Value: v.Value})
		}
		s.Enums[id] = enum
	}
	return s, nil
}

// ParseSchema reads a metadata schema kept in a file of its own, the one a
// tileset names in SchemaURI.
func ParseSchema(data []byte, o *Options) (*Schema, error) {
	if int64(len(data)) > o.maxBytes() {
		return nil, fmt.Errorf("%w: schema of %d bytes", ErrTooLarge, len(data))
	}
	var raw rawSchema
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("nt3dtiles3d: schema: %w", err)
	}
	return raw.schema()
}
