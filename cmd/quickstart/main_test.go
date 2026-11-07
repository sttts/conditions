// SPDX-FileCopyrightText: Copyright (c) 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"go/format"
	"os"
	"strings"
	"testing"
)

func readmeBlock(t *testing.T, name, language string) string {
	t.Helper()
	data, err := os.ReadFile("../../README.md")
	if err != nil {
		t.Fatal(err)
	}
	_, rest, ok := strings.Cut(string(data), "<!-- example:"+name+" -->\n```"+language+"\n")
	if !ok {
		t.Fatalf("missing README example %q", name)
	}
	block, _, ok := strings.Cut(rest, "\n```\n<!-- /example:"+name+" -->")
	if !ok {
		t.Fatalf("missing README example end %q", name)
	}
	return block + "\n"
}

func TestReadmeQuickstart(t *testing.T) {
	source, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	source = source[bytes.Index(source, []byte("package main")):]
	documented, err := format.Source([]byte(readmeBlock(t, "quickstart", "go")))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(source, documented) {
		t.Fatal("README quickstart differs from compiled cmd/quickstart/main.go")
	}
	var out bytes.Buffer
	if err := run(&out); err != nil {
		t.Fatal(err)
	}
	if want := readmeBlock(t, "quickstart-output", "text"); out.String() != want {
		t.Fatalf("README output mismatch\ngot:\n%s\nwant:\n%s", out.String(), want)
	}
}
