package sdk

import (
	"reflect"
	"testing"

	"github.com/kpetremann/jackadi/internal/proto"
	"google.golang.org/protobuf/types/known/structpb"
)

type TestOptions struct {
	Verbose    bool   `jackadi:"verbose"`
	OutputFile string `jackadi:"output_file"`
	Timeout    int    `jackadi:"timeout"`
	Region     string // No tag, fallback to field name
}

func (t *TestOptions) SetDefaults() {
	t.Verbose = false
	t.OutputFile = ""
	t.Timeout = 30
	t.Region = "us-east-1"
}

func TestHandleOptions(t *testing.T) {
	tests := map[string]struct {
		input       map[string]any
		wantErr     bool
		expectedErr string
		validate    func(t *testing.T, opts *TestOptions)
	}{
		"with jackadi tags": {
			input: map[string]any{
				"verbose":     true,
				"output_file": "/tmp/output.txt",
				"timeout":     60,
			},
			validate: func(t *testing.T, opts *TestOptions) {
				t.Helper()
				if opts.Verbose != true {
					t.Errorf("expected Verbose to be true, got %v", opts.Verbose)
				}
				if opts.OutputFile != "/tmp/output.txt" {
					t.Errorf("expected OutputFile to be '/tmp/output.txt', got %v", opts.OutputFile)
				}
				if opts.Timeout != 60 {
					t.Errorf("expected Timeout to be 60, got %v", opts.Timeout)
				}
				if opts.Region != "us-east-1" {
					t.Errorf("expected Region to be 'us-east-1' (default), got %v", opts.Region)
				}
			},
		},
		"with field name fallback": {
			input: map[string]any{
				"Region": "eu-west-1",
			},
			validate: func(t *testing.T, opts *TestOptions) {
				t.Helper()
				if opts.Region != "eu-west-1" {
					t.Errorf("expected Region to be 'eu-west-1', got %v", opts.Region)
				}
			},
		},
		"with invalid option": {
			input: map[string]any{
				"invalid_option": "value",
			},
			wantErr:     true,
			expectedErr: "invalid 'invalid_option' option",
		},
		"with no options": {
			input: nil,
			validate: func(t *testing.T, opts *TestOptions) {
				t.Helper()
				if opts.Verbose != false {
					t.Errorf("expected Verbose to be false (default), got %v", opts.Verbose)
				}
				if opts.Timeout != 30 {
					t.Errorf("expected Timeout to be 30 (default), got %v", opts.Timeout)
				}
				if opts.Region != "us-east-1" {
					t.Errorf("expected Region to be 'us-east-1' (default), got %v", opts.Region)
				}
			},
		},
		"mixed tags and field names": {
			input: map[string]any{
				"verbose": true,
				"Region":  "ap-south-1",
				"timeout": 120,
			},
			validate: func(t *testing.T, opts *TestOptions) {
				t.Helper()
				if opts.Verbose != true {
					t.Errorf("expected Verbose to be true, got %v", opts.Verbose)
				}
				if opts.Region != "ap-south-1" {
					t.Errorf("expected Region to be 'ap-south-1', got %v", opts.Region)
				}
				if opts.Timeout != 120 {
					t.Errorf("expected Timeout to be 120, got %v", opts.Timeout)
				}
			},
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Helper()
			var options *structpb.Struct
			var err error

			if tt.input != nil {
				options, err = structpb.NewStruct(tt.input)
				if err != nil {
					t.Fatalf("failed to create structpb: %v", err)
				}
			}

			input := &proto.Input{
				Options: options,
			}

			result, err := handleOptions(reflect.TypeFor[TestOptions](), input)

			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				if tt.expectedErr != "" && err.Error() != tt.expectedErr {
					t.Errorf("expected error '%s', got '%s'", tt.expectedErr, err.Error())
				}
				return
			}

			if err != nil {
				t.Fatalf("handleOptions failed: %v", err)
			}

			opts, ok := result.Interface().(*TestOptions)
			if !ok {
				t.Fatalf("result is not *TestOptions")
			}

			if tt.validate != nil {
				tt.validate(t, opts)
			}
		})
	}
}
