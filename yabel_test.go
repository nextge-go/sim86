package main

import (
	"io/ioutil"
	"testing"
)

func TestIP(t *testing.T) {
	ip := uint16(0x1234)
	S(IP, ip)
	if G16(IP) != ip {
		t.Errorf("ip: got %04x, want %04x", G(IP), ip)
		x86emu_dump_regs()
	}
}

func TestEAX(t *testing.T) {
	eax := uint32(0xdeadbeef)
	S(EAX, eax)
	S(AL, uint8(0xaa))
	S(AH, uint8(0x44))
	if G32(EAX) != 0x44aa {
		t.Errorf("EAX: got %08x, want %08x", G(EAX), 0x44aa)
		x86emu_dump_xregs()
	}
	// TODO: change mode, check G again
}

type regval struct {
	r regtype
	v uint32
}

type check struct {
	n string
	r []regval
}

func TestBinary(t *testing.T) {
	var checks = []check{
		{n: "Halt", r: []regval{{IP, 1}, {SP, 0x2000}}},
		{n: "seg", r: []regval{{AX, 0x23}, {SS, 0x20}, {ES, 0x21}, {FS, 0x22}, {IP, 0x13}}},
		{n: "jmpcsip", r: []regval{{CS, 0x2}, {IP, 0x1}}},
		{n: "pushpop", r: []regval{{EBX, 0x12345678}, {CX, 0x1234}, {EDX, 0x12345678},}},
		{n: "qemu-test-i386-1", r: []regval{{CS, 0x2}, {IP, 0x8}, {EAX, 1}}},
		{n: "qemu-test-i386-2", r: []regval{{CS, 0x2}, {IP, 0x1a}, {EAX, 1}, {EBX, 0x12345678}, {ECX, 0x2000},}},
		{n: "qemu-test-i386-3", r: []regval{{CS, 0x2}, {IP, 0x1}}},
		{n: "qemu-test-i386-4", r: []regval{{CS, 0x2}, {IP, 0x8}}},
	}

	b, err := ioutil.ReadFile("test.bin")
	if err != nil {
		t.Fatal(err)
	}
	copy(memory[:], b)
	S(CS, uint16(0))
	S(IP, uint16(0))
	S(SP, uint16(0x2000))
	M.x86.debug |= DEBUG_DISASSEMBLE_F | DEBUG_DECODE_F | DEBUG_TRACE_F
	for _, c := range checks {
		S(SS, uint16(0))
		t.Logf("Start Test %s", c.n)
		X86EMU_exec()
		for i, r := range c.r {
			if G32(r.r) != r.v {
				t.Errorf("%v: %d'th test fails: reg %04x got %04x, want %04x", c.n, i, r.r, G32(r.r), r.v)
			}
		}
		t.Logf("Done Test %s", c.n)
	}
}