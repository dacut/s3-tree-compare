package main

import "strings"

type StringListFlag struct {
	Values []string
	Join   string
}

func (slf *StringListFlag) String() string {
	if slf.Values == nil {
		return ""
	}

	join := slf.Join
	if join == "" {
		join = ","
	}

	return strings.Join(slf.Values, join)
}

func (slf *StringListFlag) Set(value string) error {
	slf.Values = append(slf.Values, value)
	return nil
}
