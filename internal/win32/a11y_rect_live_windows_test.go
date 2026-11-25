// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build windows && integration

package win32

import (
	"bufio"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// uiaRects walks the children of the demo window as a UI Automation CLIENT and
// prints "W x y w h" for the window and "C name|x y w h" for each child.
const uiaRects = `
Add-Type -AssemblyName UIAutomationClient,UIAutomationTypes
$A = [System.Windows.Automation.AutomationElement]
$sc = [System.Windows.Automation.TreeScope]::Children
$cond = New-Object System.Windows.Automation.PropertyCondition($A::NameProperty, 'go-widgets/window demo')
$w = $null
for ($i = 0; $i -lt 40 -and $null -eq $w; $i++) {
	$w = $A::RootElement.FindFirst($sc, $cond)
	if ($null -eq $w) { Start-Sleep -Milliseconds 500 }
}
if ($null -eq $w) { 'NOWINDOW'; exit 0 }
$kids = $null
for ($i = 0; $i -lt 40; $i++) {
	$kids = $w.FindAll($sc, [System.Windows.Automation.Condition]::TrueCondition)
	if ($kids.Count -gt 0) { break }
	Start-Sleep -Milliseconds 500
}
$r = $w.Current.BoundingRectangle
'W {0} {1} {2} {3}' -f [int]$r.X, [int]$r.Y, [int]$r.Width, [int]$r.Height
foreach ($k in $kids) {
	$r = $k.Current.BoundingRectangle
	if ([double]::IsInfinity($r.X)) { 'C {0}|inf' -f $k.Current.Name; continue }
	'C {0}|{1} {2} {3} {4}' -f $k.Current.Name, [int]$r.X, [int]$r.Y, [int]$r.Width, [int]$r.Height
}
`

// TestLiveUIAChildRectanglesReachTheClient is the regression test for
// go-widgets/window#188. A UI Automation client asking a drawn element for its
// BoundingRectangle must get the rectangle the provider wrote, not (0,0 0x0).
// On windows/arm64 it was zeros because the runtime's callback entry did not
// preserve D8-D15, which UI Automation's core holds the rectangle in; on
// amd64 it already passed, which is why the arm64 lane is the one that bites.
//
// The witness is a real client (System.Windows.Automation through PowerShell),
// not our own reader of what the provider wrote.
func TestLiveUIAChildRectanglesReachTheClient(t *testing.T) {
	skipUnlessLive(t)
	// WINDOW_WIN32_DEMO names a prebuilt demo, for a host without a Go toolchain.
	exe := os.Getenv("WINDOW_WIN32_DEMO")
	if exe == "" {
		exe = filepath.Join(t.TempDir(), "windowdemo.exe")
		build := exec.Command("go", "build", "-o", exe, "../../cmd/windowdemo")
		build.Env = append(os.Environ(), "CGO_ENABLED=0")
		if out, err := build.CombinedOutput(); err != nil {
			t.Fatalf("build windowdemo: %v\n%s", err, out)
		}
	}
	demo := exec.Command(exe)
	if err := demo.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = demo.Process.Kill(); _ = demo.Wait() })
	time.Sleep(2 * time.Second)

	out := powershell(t, uiaRects)
	if strings.Contains(out, "NOWINDOW") {
		t.Fatalf("UI Automation never listed the demo window:\n%s", out)
	}
	var win [4]int
	var kids int
	sc := bufio.NewScanner(strings.NewReader(out))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		switch {
		case strings.HasPrefix(line, "W "):
			win = parseRect(t, line[2:])
		case strings.HasPrefix(line, "C "):
			name, rect, _ := strings.Cut(line[2:], "|")
			if rect == "inf" {
				t.Errorf("child %q: the client got no rectangle", name)
				continue
			}
			r := parseRect(t, rect)
			kids++
			if r[2] <= 0 || r[3] <= 0 {
				t.Errorf("child %q: BoundingRectangle %v is empty, want the element's screen rectangle", name, r)
				continue
			}
			if r[0] < win[0] || r[1] < win[1] || r[0]+r[2] > win[0]+win[2] || r[1]+r[3] > win[1]+win[3] {
				t.Errorf("child %q: BoundingRectangle %v is not inside the window %v", name, r, win)
			}
		}
	}
	if kids == 0 {
		t.Fatalf("the client saw no child of the window:\n%s", out)
	}
}

func parseRect(t *testing.T, s string) (r [4]int) {
	t.Helper()
	f := strings.Fields(s)
	if len(f) != 4 {
		t.Fatalf("not a rectangle: %q", s)
	}
	for i := range r {
		n, err := strconv.Atoi(f[i])
		if err != nil {
			t.Fatalf("not a rectangle: %q", s)
		}
		r[i] = n
	}
	return r
}
