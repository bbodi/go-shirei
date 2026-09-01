package perfcore

import "testing"

func TestFormatKBStepsUpAUnit(t *testing.T) {
	cases := []struct {
		in   float64
		want string
	}{
		{0, "0.0 KB"},
		{1.1, "1.1 KB"},
		{96.2, "96.2 KB"},
		{412.5, "412 KB"},
		{2036, "2.0 MB"},
		{1024 * 1024 * 3, "3.0 GB"},
	}
	for _, c := range cases {
		if got := FormatKB(c.in); got != c.want {
			t.Errorf("FormatKB(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestFormatBytesAndMB(t *testing.T) {
	if got := FormatBytes(812345); got != "793 KB" {
		t.Errorf("FormatBytes(812345) = %q", got)
	}
	if got := FormatMB(2048); got != "2.0 GB" {
		t.Errorf("FormatMB(2048) = %q", got)
	}
	// A zero keeps the unit the caller was working in, so a blank row still
	// lines up with a full one.
	if got := FormatMB(0); got != "0.0 MB" {
		t.Errorf("FormatMB(0) = %q", got)
	}
}

func TestFormatMs(t *testing.T) {
	cases := []struct {
		in   float64
		want string
	}{
		{0, "0 ms"},
		{0.42, "0.42 ms"},
		{3, "3.0 ms"},
		{16.03, "16.0 ms"},
		{1500, "1.5 s"},
	}
	for _, c := range cases {
		if got := FormatMs(c.in); got != c.want {
			t.Errorf("FormatMs(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestFormatCountGroupsThousands(t *testing.T) {
	cases := []struct {
		in   float64
		want string
	}{
		{0, "0"},
		{999, "999"},
		{1000, "1,000"},
		{12480, "12,480"},
		{1234567, "1,234,567"},
		{-4200, "-4,200"},
	}
	for _, c := range cases {
		if got := FormatCount(c.in); got != c.want {
			t.Errorf("FormatCount(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}
