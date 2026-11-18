package gnosis

import "testing"

func TestParseAutomationPID(t *testing.T) {
	tests := []struct {
		name    string
		output  string
		want    int
		wantErr bool
	}{
		{name: "echoed pid", output: "4242\n", want: 4242},
		{name: "surrounding whitespace", output: "  17\r\n", want: 17},
		{name: "empty output", output: "", wantErr: true},
		{name: "zero is not a process", output: "0", wantErr: true},
		{name: "negative", output: "-3", wantErr: true},
		{name: "non numeric", output: "nohup: ignoring input", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseAutomationPID(tt.output)
			if (err != nil) != tt.wantErr {
				t.Fatalf("parseAutomationPID(%q) error = %v, wantErr %v", tt.output, err, tt.wantErr)
			}
			if got != tt.want {
				t.Fatalf("parseAutomationPID(%q) = %d, want %d", tt.output, got, tt.want)
			}
		})
	}
}
