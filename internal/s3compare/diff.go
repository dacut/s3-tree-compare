package s3compare

import "fmt"

type OutputFormat int

const (
	OutputFormatText OutputFormat = iota
	OutputFormatJSON
)

type DiffObject struct {
	URL          string `json:"Url"`
	LastModified string `json:"LastModified,omitempty"`
}

type DiffType string

const DiffTypeMissing DiffType = DiffType("Missing")
const DiffTypeMismatch DiffType = DiffType("Mismatch")

type DiffReport struct {
	Type          DiffType            `json:"Type"`
	Objects       []DiffObject        `json:"DiffObjects"`
	CommonHeaders map[string]string   `json:"CommonHeaders,omitempty"`
	DiffHeaders   map[string][]string `json:"DiffHeaders,omitempty"`
}

type DiffObjectPosition int

const (
	FirstObject DiffObjectPosition = iota
	SecondObject
)

func MissingDiffReport(url string, position DiffObjectPosition) *DiffReport {
	empty := DiffObject{URL: ""}
	obj := DiffObject{URL: url}

	var objects []DiffObject

	switch position {
	case FirstObject:
		objects = []DiffObject{obj, empty}
	case SecondObject:
		objects = []DiffObject{empty, obj}
	default:
		panic(fmt.Errorf("invalid value for position: %d", position))
	}

	return &DiffReport{
		Type:    DiffTypeMissing,
		Objects: objects,
	}
}
