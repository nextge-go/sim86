package main

var Gen_reg_t i386_general_regs
var _X86EMU_env X86EMU_sysEnv
var _X86EMU_intrTab [][]byte
var DEBUG_SYS_F uint32

var (
	x86emu_optab2 = make(map[uint8]func(uint8))
)
