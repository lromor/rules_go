package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

type scanner interface {
	Scan() bool
	Text() string
	Err() error
}

type sliceScanner struct {
	tokens    []string
	lastIndex int
}

func newSliceScanner(tokens []string) *sliceScanner {
	return &sliceScanner{tokens, -1}
}

func (s *sliceScanner) Scan() bool {
	if s.lastIndex < len(s.tokens)-1 {
		s.lastIndex++
		return true
	}
	return false
}

func (s *sliceScanner) Text() string {
	if s.lastIndex < 0 {
		return ""
	}
	return s.tokens[s.lastIndex]
}

func (s *sliceScanner) Err() error {
	return nil
}

type arg struct {
	Setter func(value string)
}

type paramsReaderGetter func(paramsPath string) (io.Reader, error)

type bazelArgsParser struct {
	args               map[string]arg
	paramsReaderGetter paramsReaderGetter
}

func (p *bazelArgsParser) WithParamsReaderGetter(getter paramsReaderGetter) *bazelArgsParser {
	p.paramsReaderGetter = getter
	return p
}

func (p *bazelArgsParser) WithArg(flag string, setter func(value string)) *bazelArgsParser {
	arg := arg{
		Setter:   setter,
	}
	p.args[flag] = arg
	return p
}

func (p *bazelArgsParser) parseBazelArguments(scanner scanner) error {
	var currentSetter func(value string)
	for scanner.Scan() {
		token := scanner.Text()
		arg, ok := p.args[token]
		if ok {
			currentSetter = arg.Setter
			continue
		}
		// We always expect to have a flag as first token.
		if currentSetter == nil {
			return errors.New(fmt.Sprintf("unexpected flag %s", token))
		}
		currentSetter(token)
	}
	return nil
}

func (p *bazelArgsParser) Parse(arguments []string) error {
	if len(arguments) == 0 {
		return nil
	}
	firstArgument := arguments[0]

	// If the first argument is not a param argument,
	// read the tokens from the arguments.
	if firstArgument[0] != '@' {
		scanner := newSliceScanner(arguments)
		return p.parseBazelArguments(scanner)
	}
	if len(arguments) > 1 {
		return errors.New("expected single argument with param file")
	}

	// We have a param argument, read tokens from the paramsReader
	// using the param argument value.
	paramsReader, err := p.paramsReaderGetter(firstArgument[1:])
	if err != nil {
		return err
	}
	scanner := bufio.NewScanner(paramsReader)
	return p.parseBazelArguments(scanner)
}

func NewBazelArgsParser() *bazelArgsParser {
	return &bazelArgsParser{
		args: map[string]arg{},
		paramsReaderGetter: func(value string) (io.Reader, error) {
			return os.Open(value)
		},
	}
}

type archive struct {
	ID              string   `json:"ID"`
	PkgPath         string   `json:"PkgPath"`
	ExportFile      string   `json:"ExportFile"`
	GoFiles         []string `json:"GoFiles"`
	CompiledGoFiles []string `json:"CompiledGoFiles"`
	OtherFiles      []string `json:"OtherFiles"`
}

// The bazel args will be parsed either from the program args or
// from a file if its path prepended with "@" is provided.
func parseArchiveAndOutputPath(arguments []string, archive *archive) (string, error) {
	var outputPath string
	parser := NewBazelArgsParser().
		WithArg("--id", func(value string) {
			archive.ID = value }).
		WithArg("--pkg-path", func(value string) {
			archive.PkgPath = value }).
		WithArg("--export-file", func(value string) {
			archive.ExportFile = value }).
		WithArg("--orig-srcs", func(value string) {
			if strings.HasSuffix(value, ".go") {
				archive.GoFiles = append(archive.GoFiles, value)
			} else {
				archive.GoFiles = append(archive.OtherFiles, value)
			}
		}).
		WithArg("--data-srcs", func(value string) {
			if strings.HasSuffix(value, ".go") {
 				archive.CompiledGoFiles = append(archive.CompiledGoFiles, value)
 			}
		}).
		WithArg("--output-file", func(value string) {
			outputPath = value
		})
	err := parser.Parse(arguments)
	if err != nil {
		return "", err
	}
	if outputPath == "" {
		return "", errors.New("invalid usage: no output path")
	}
	return outputPath, err
}

// Parse bazel generated arguments encoding an archive contents and generate
// a corresponding json file.
func main() {
	var archive archive
	outputPath, err := parseArchiveAndOutputPath(
		os.Args[1:],
		&archive,
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v", err)
		os.Exit(1)
	}
	f, err := os.Create(outputPath)
	defer f.Close()
	if err := json.NewEncoder(f).Encode(archive); err != nil {
		fmt.Fprintf(os.Stderr, "unable to encode archive: %v", err)
		os.Exit(1)
	}
}
