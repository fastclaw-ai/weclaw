package agent

import (
	"reflect"
	"testing"
)

func TestMergeEnvOverridesAndAppends(t *testing.T) {
	base := []string{"PATH=/usr/bin", "KEEP=1", "DUP=old"}
	extra := map[string]string{
		"NEW":   "2",
		"DUP":   "new",
		"EMPTY": "",
	}

	got, err := mergeEnv(base, extra)
	if err != nil {
		t.Fatalf("mergeEnv returned error: %v", err)
	}

	want := []string{"PATH=/usr/bin", "KEEP=1", "DUP=new", "EMPTY=", "NEW=2"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("mergeEnv() = %#v, want %#v", got, want)
	}
}

func TestMergeEnvRejectsInvalidKey(t *testing.T) {
	_, err := mergeEnv(nil, map[string]string{"BAD=KEY": "value"})
	if err == nil {
		t.Fatal("mergeEnv() error = nil, want invalid env key error")
	}
}

func TestMergeEnvPreservesBaseWhenNoExtra(t *testing.T) {
	base := []string{"PATH=/usr/bin", "KEEP=1"}

	got, err := mergeEnv(base, nil)
	if err != nil {
		t.Fatalf("mergeEnv returned error: %v", err)
	}
	if !reflect.DeepEqual(got, base) {
		t.Fatalf("mergeEnv() = %#v, want %#v", got, base)
	}
}

func TestMergeEnvRejectsEmptyKey(t *testing.T) {
	_, err := mergeEnv(nil, map[string]string{"": "value"})
	if err == nil {
		t.Fatal("mergeEnv() error = nil, want empty env key error")
	}
}

func TestMergeEnvOverridesExistingKeyWithEmptyValue(t *testing.T) {
	got, err := mergeEnv([]string{"EMPTY=old"}, map[string]string{"EMPTY": ""})
	if err != nil {
		t.Fatalf("mergeEnv returned error: %v", err)
	}
	want := []string{"EMPTY="}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("mergeEnv() = %#v, want %#v", got, want)
	}
}
