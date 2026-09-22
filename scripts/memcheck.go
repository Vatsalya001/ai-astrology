// A memory test that does not need a reboot.
//
// `go run scripts/memcheck.go [gigabytes] [minutes]`
//
// ── Why this exists ──
//
// This repository has recorded fourteen data-corruption events, including
// two single-bit clears in a committed 16.8 MB ephemeris kernel and two
// more in a golden dasha fixture. The proper tool is memtest86+, which is
// installed — and it needs a reboot into GRUB, which is not available on
// a managed office laptop.
//
// So: the same idea, from userspace, while you keep working.
//
// ── What it can and cannot do, plainly ──
//
// CAN: allocate a large buffer, write known patterns, read them back, and
// report any byte that changed. A bit that flips while this holds the page
// is caught with its address and its before/after value.
//
// CANNOT: test memory the kernel and other processes are using — which on
// a 40 GB machine with 5 GB free is most of it. It also cannot test the
// physical cells behind any page the kernel decides to move or swap.
// memtest86+ owns the whole machine and therefore tests all of it; this
// tests the slice it can borrow.
//
// So a clean run here is WEAKER evidence than a clean memtest86+ pass. It
// is not nothing: a marginal cell that flips under sustained access has a
// real chance of being inside a multi-gigabyte working set, and the
// corruptions this repo saw were single-bit clears of exactly that kind.
//
// A DIRTY run, by contrast, is conclusive. Userspace memory does not
// change on its own.
package main

import (
	"fmt"
	"math/rand"
	"os"
	"runtime"
	"strconv"
	"time"
)

// Patterns chosen so every bit is driven both ways, and so adjacent cells
// hold opposing values — the condition under which a weak cell is most
// likely to be disturbed by its neighbour.
var patterns = []byte{0x00, 0xFF, 0x55, 0xAA, 0x0F, 0xF0}

func main() {
	gb := 4
	minutes := 10
	if len(os.Args) > 1 {
		if v, err := strconv.Atoi(os.Args[1]); err == nil && v > 0 {
			gb = v
		}
	}
	if len(os.Args) > 2 {
		if v, err := strconv.Atoi(os.Args[2]); err == nil && v > 0 {
			minutes = v
		}
	}

	size := gb << 30
	fmt.Printf("allocating %d GiB, running for %d minute(s)\n", gb, minutes)
	fmt.Printf("Ctrl-C is safe at any point.\n\n")

	buf := make([]byte, size)

	// Touch every page once so the allocation is real rather than a
	// promise the kernel has not honoured yet. Without this the first
	// pass measures page-fault handling, not memory.
	for i := 0; i < size; i += 4096 {
		buf[i] = 1
	}
	runtime.GC()

	deadline := time.Now().Add(time.Duration(minutes) * time.Minute)
	pass, errors := 0, 0

	for time.Now().Before(deadline) {
		for _, p := range patterns {
			if time.Now().After(deadline) {
				break
			}
			pass++
			fill(buf, p)
			errors += verify(buf, p, pass)
		}

		// A pseudo-random pass, seeded per round. A fixed pattern can sit
		// entirely inside a cache line that never reaches the DRAM cell
		// being suspected; varying bytes defeats that.
		if time.Now().Before(deadline) {
			pass++
			seed := time.Now().UnixNano()
			fillRandom(buf, seed)
			errors += verifyRandom(buf, seed, pass)
		}

		fmt.Printf("  %d passes over %d GiB — %d error(s), %s remaining\n",
			pass, gb, errors, time.Until(deadline).Round(time.Second))
	}

	fmt.Println()
	if errors == 0 {
		fmt.Printf("✓ %d passes, no errors in the %d GiB this process could borrow.\n", pass, gb)
		fmt.Println()
		fmt.Println("This does NOT clear the machine. It could not test memory held by the")
		fmt.Println("kernel or other processes, which is most of it. memtest86+ remains the")
		fmt.Println("only way to test all of it, and it needs a reboot.")
		os.Exit(0)
	}

	fmt.Printf("✗ %d ERROR(S) across %d passes.\n\n", errors, pass)
	fmt.Println("Userspace memory does not change on its own. This is conclusive:")
	fmt.Println("this machine has a memory fault. Stop trusting anything it computed,")
	fmt.Println("and escalate to IT with the addresses above.")
	os.Exit(1)
}

func fill(buf []byte, p byte) {
	for i := range buf {
		buf[i] = p
	}
}

func verify(buf []byte, want byte, pass int) int {
	bad := 0
	for i, got := range buf {
		if got != want {
			report(pass, i, want, got, &bad)
		}
	}
	return bad
}

func fillRandom(buf []byte, seed int64) {
	r := rand.New(rand.NewSource(seed))
	for i := range buf {
		buf[i] = byte(r.Intn(256))
	}
}

func verifyRandom(buf []byte, seed int64, pass int) int {
	r := rand.New(rand.NewSource(seed))
	bad := 0
	for i, got := range buf {
		want := byte(r.Intn(256))
		if got != want {
			report(pass, i, want, got, &bad)
		}
	}
	return bad
}

// Capped at ten lines. A failing DIMM can produce millions, and scrolling
// them off the screen destroys the addresses that identify it.
func report(pass, offset int, want, got byte, bad *int) {
	*bad++
	if *bad <= 10 {
		fmt.Printf("  MISMATCH pass %d offset %d: wrote 0x%02X read 0x%02X (xor 0x%02X)\n",
			pass, offset, want, got, want^got)
	} else if *bad == 11 {
		fmt.Println("  ... further mismatches suppressed")
	}
}
