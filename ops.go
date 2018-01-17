/****************************************************************************
*
*						Realmode X86 Emulator Library
*
*            	Copyright (C) 1991-2004 SciTech Software, Inc.
* 				     Copyright (C) David Mosberger-Tang
* 					   Copyright (C) 1999 Egbert Eich
*
*  ========================================================================
*
*  Permission to use, copy, modify, distribute, and sell this software and
*  its documentation for any purpose is hereby granted without fee,
*  provided that the above copyright notice appear in all copies and that
*  both that copyright notice and this permission notice appear in
*  supporting documentation, and that the name of the authors not be used
*  in advertising or publicity pertaining to distribution of the software
*  without specific, written prior permission.  The authors makes no
*  representations about the suitability of this software for any purpose.
*  It is provided "as is" without express or implied warranty.
*
*  THE AUTHORS DISCLAIMS ALL WARRANTIES WITH REGARD TO THIS SOFTWARE,
*  INCLUDING ALL IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS, IN NO
*  EVENT SHALL THE AUTHORS BE LIABLE FOR ANY SPECIAL, INDIRECT OR
*  CONSEQUENTIAL DAMAGES OR ANY DAMAGES WHATSOEVER RESULTING FROM LOSS OF
*  USE, DATA OR PROFITS, WHETHER IN AN ACTION OF CONTRACT, NEGLIGENCE OR
*  OTHER TORTIOUS ACTION, ARISING OUT OF OR IN CONNECTION WITH THE USE OR
*  PERFORMANCE OF THIS SOFTWARE.
*
*  ========================================================================
*
* Language:		ANSI C
* Environment:	Any
* Developer:    Kendall Bennett
*
* Description:  This file includes subroutines to implement the decoding
*               and emulation of all the x86 processor instructions.
*
* There are approximately 250 subroutines in here, which correspond
* to the 256 byte-"opcodes" found on the 8086.  The table which
* dispatches this is found in the files optab.[ch].
*
* Each opcode proc has a comment preceding it which gives it's table
* address.  Several opcodes are missing (undefined) in the table.
*
* Each proc includes information for decoding (DECODE_PRINTF and
* DECODE_PRINTF2), debugging (TRACE_REGS, SINGLE_STEP), and misc
* functions (START_OF_INSTR, END_OF_INSTR).
*
* Many of the procedures are *VERY* similar in coding.  This has
* allowed for a very large amount of code to be generated in a fairly
* short amount of time (i.e. cut, paste, and modify).  The result is
* that much of the code below could have been folded into subroutines
* for a large reduction in size of this file.  The downside would be
* that there would be a penalty in execution speed.  The file could
* also have been *MUCH* larger by inlining certain functions which
* were called.  This could have resulted even faster execution.  The
* prime directive I used to decide whether to inline the code or to
* modularize it, was basically: 1) no unnecessary subroutine calls,
* 2) no routines more than about 200 lines in size, and 3) modularize
* any code that I might not get right the first time.  The fetch_*
* subroutines fall into the latter category.  The The decode_* fall
* into the second category.  The coding of the "switch(mod){ .... }"
* in many of the subroutines below falls into the first category.
* Especially, the coding of {add,and,or,sub,...}_{byte,word}
* subroutines are an especially glaring case of the third guideline.
* Since so much of the code is cloned from other modules (compare
* opcode #00 to opcode #01), making the basic operations subroutine
* calls is especially important; otherwise mistakes in coding an
* "add" would represent a nightmare in maintenance.
*
****************************************************************************/


/*----------------------------- Implementation ----------------------------*/

/* constant arrays to do several instructions in just one function */
package main

var x86emu_GenOpName = []string {
    "ADD", "OR", "ADC", "SBB", "AND", "SUB", "XOR", "CMP"};

// :g/^var/s/\(u.[624]*\)\(.*\)=/\2 \1 = 

/* used by several opcodes  */
var genop_byte_operation= []func(u8 d, u8 s) u8 {
    add_byte, /* 00 */
    or_byte, /* 01 */
    adc_byte, /* 02 */
    sbb_byte, /* 03 */
    and_byte, /* 04 */
    sub_byte, /* 05 */
    xor_byte, /* 06 */
    cmp_byte, /* 07 */
};

var genop_word_operation= []func(u16 d, u16 s) u16 {
    add_word, /*00 */
    or_word, /*01 */
    adc_word, /*02 */
    sbb_word, /*03 */
    and_word, /*04 */
    sub_word, /*05 */
    xor_word, /*06 */
    cmp_word, /*07 */
};

var genop_long_operation= []func(u32 d, u32 s) u32 {
    add_long, /*00 */
    or_long, /*01 */
    adc_long, /*02 */
    sbb_long, /*03 */
    and_long, /*04 */
    sub_long, /*05 */
    xor_long, /*06 */
    cmp_long, /*07 */
};

/* used by opcodes 80, c0, d0, and d2. */
var opcD0_byte_operation= []func(u8 d, u8 s) u8 {
    rol_byte,
    ror_byte,
    rcl_byte,
    rcr_byte,
    shl_byte,
    shr_byte,
    shl_byte, /* sal_byte === shl_byte  by definition */
    sar_byte,
};

/* used by opcodes c1, d1, and d3. */
var opcD1_word_operation= []func(u16 s, u8 d) u16 {
    rol_word,
    ror_word,
    rcl_word,
    rcr_word,
    shl_word,
    shr_word,
    shl_word, /* sal_byte === shl_byte  by definition */
    sar_word,
};

/* used by opcodes c1, d1, and d3. */
var opcD1_long_operation= []func(u32 s, u8 d) u32 {
    rol_long,
    ror_long,
    rcl_long,
    rcr_long,
    shl_long,
    shr_long,
    shl_long, /* sal_byte === shl_byte  by definition */
    sar_long,
};



var opF6_names = []string {
   "TEST\t", "", "NOT\t", "NEG\t", "MUL\t", "IMUL\t", "DIV\t", "IDIV\t" };



/****************************************************************************
PARAMETERS:
op1 - Instruction op code

REMARKS:
Handles illegal opcodes.
****************************************************************************/
func x86emuOp_illegal_op( u8 op1) {
    START_OF_INSTR();
    if (M.x86.R_SP != 0) {
        DECODE_PRINTF("ILLEGAL X86 OPCODE\n");
        TRACE_REGS();
        printf("%04x:%04x: %02X ILLEGAL X86 OPCODE!\n", M.x86.R_CS, M.x86.R_IP-1,op1)
                                          ;
        HALT_SYS();
        }    else {
        /* If we get here, it means the stack pointer is back to zero
         * so we are just returning from an emulator service call
         * so therte is no need to display an error message. We trap
         * the emulator with an 0xF1 opcode to finish the service
         * call.
         */
        X86EMU_halt_sys();
        }
    END_OF_INSTR();
}

/****************************************************************************
REMARKS:
Handles opcodes 0x00, 0x08, 0x10, 0x18, 0x20, 0x28, 0x30, 0x38
****************************************************************************/
func x86emuOp_genop_byte_RM_R(u8 op1) {
    var mod, rl, rh int
    var destoffset uint
    var destreg, srcreg *u8
    var destval u8

    op1 = (op1 >> 3) & 0x7;

    START_OF_INSTR();
    DECODE_PRINTF(x86emu_GenOpName[op1]);
    DECODE_PRINTF("\t");
    FETCH_DECODE_MODRM(mod, rh, rl);
	if (mod<3)        {
		destoffset = decode_rmXX_address(mod,rl);
		DECODE_PRINTF(",");
		destval = fetch_data_byte(destoffset);
		srcreg = DECODE_RM_BYTE_REGISTER(rh);
		DECODE_PRINTF("\n");
		TRACE_AND_STEP();
		destval = genop_byte_operation[op1](destval, *srcreg);
		if (op1 != 7){
			store_data_byte(destoffset, destval);
		}
        }    else        { /* register to register */
		destreg = DECODE_RM_BYTE_REGISTER(rl);
		DECODE_PRINTF(",");
		srcreg = DECODE_RM_BYTE_REGISTER(rh);
		DECODE_PRINTF("\n");
		TRACE_AND_STEP();
		*destreg = genop_byte_operation[op1](*destreg, *srcreg);
        }
    DECODE_CLEAR_SEGOVR();
    END_OF_INSTR();
}

/****************************************************************************
REMARKS:
Handles opcodes 0x01, 0x09, 0x11, 0x19, 0x21, 0x29, 0x31, 0x39
****************************************************************************/
func x86emuOp_genop_word_RM_R(u8 op1) {
    var mod, rl, rh int
    var destoffset uint

    op1 = (op1 >> 3) & 0x7;

    START_OF_INSTR();
    DECODE_PRINTF(x86emu_GenOpName[op1]);
    DECODE_PRINTF("\t");
    FETCH_DECODE_MODRM(mod, rh, rl);

    if (mod<3) {
        destoffset = decode_rmXX_address(mod,rl);
        if (M.x86.mode & SYSMODE_PREFIX_DATA) {
            var destval u32
            var srcreg *u32

            DECODE_PRINTF(",");
            destval = fetch_data_long(destoffset);
            srcreg = DECODE_RM_LONG_REGISTER(rh);
            DECODE_PRINTF("\n");
            TRACE_AND_STEP();
            destval = genop_long_operation[op1](destval, *srcreg);
            if (op1 != 7){
		    store_data_long(destoffset, destval);
	    }
        } else {
            var destval u16
            var srcreg *u16

            DECODE_PRINTF(",");
            destval = fetch_data_word(destoffset);
            srcreg = DECODE_RM_WORD_REGISTER(rh);
            DECODE_PRINTF("\n");
            TRACE_AND_STEP();
            destval = genop_word_operation[op1](destval, *srcreg);
            if (op1 != 7){
		    store_data_word(destoffset, destval);
	    }
        }
    } else { /* register to register */
        if (M.x86.mode & SYSMODE_PREFIX_DATA) {
            var destreg, srcreg *u32

            destreg = DECODE_RM_LONG_REGISTER(rl);
            DECODE_PRINTF(",");
            srcreg = DECODE_RM_LONG_REGISTER(rh);
            DECODE_PRINTF("\n");
            TRACE_AND_STEP();
            *destreg = genop_long_operation[op1](*destreg, *srcreg);
        } else {
            var destreg, srcreg *u16

            destreg = DECODE_RM_WORD_REGISTER(rl);
            DECODE_PRINTF(",");
            srcreg = DECODE_RM_WORD_REGISTER(rh);
            DECODE_PRINTF("\n");
            TRACE_AND_STEP();
            *destreg = genop_word_operation[op1](*destreg, *srcreg);
        }
    }
    DECODE_CLEAR_SEGOVR();
    END_OF_INSTR();
}

/****************************************************************************
REMARKS:
Handles opcodes 0x02, 0x0a, 0x12, 0x1a, 0x22, 0x2a, 0x32, 0x3a
****************************************************************************/
func x86emuOp_genop_byte_R_RM(u8 op1) {
    var mod, rl, rh int
    var destreg, srcreg *u8
    var srcoffset uint
    var srcval u8

    op1 = (op1 >> 3) & 0x7;

    START_OF_INSTR();
    DECODE_PRINTF(x86emu_GenOpName[op1]);
    DECODE_PRINTF("\t");
    FETCH_DECODE_MODRM(mod, rh, rl);
    if (mod < 3) {
        destreg = DECODE_RM_BYTE_REGISTER(rh);
        DECODE_PRINTF(",");
        srcoffset = decode_rmXX_address(mod,rl);
        srcval = fetch_data_byte(srcoffset);
    } else { /* register to register */
        destreg = DECODE_RM_BYTE_REGISTER(rh);
        DECODE_PRINTF(",");
        srcreg = DECODE_RM_BYTE_REGISTER(rl);
        srcval = *srcreg;
    }
    DECODE_PRINTF("\n");
    TRACE_AND_STEP();
    *destreg = genop_byte_operation[op1](*destreg, srcval);

    DECODE_CLEAR_SEGOVR();
    END_OF_INSTR();
}

/****************************************************************************
REMARKS:
Handles opcodes 0x03, 0x0b, 0x13, 0x1b, 0x23, 0x2b, 0x33, 0x3b
****************************************************************************/
func x86emuOp_genop_word_R_RM(u8 op1) {
    var mod, rl, rh int
    var srcoffset uint
    var destreg *u32; var srcreg u32
    var destreg *u16

    op1 = (op1 >> 3) & 0x7;

    START_OF_INSTR();
    DECODE_PRINTF(x86emu_GenOpName[op1]);
    DECODE_PRINTF("\t");
    FETCH_DECODE_MODRM(mod, rh, rl);
    if (mod < 3) {
        srcoffset = decode_rmXX_address(mod,rl);
        if (M.x86.mode & SYSMODE_PREFIX_DATA) {
            destreg32 = DECODE_RM_LONG_REGISTER(rh);
            DECODE_PRINTF(",");
            srcval = fetch_data_long(srcoffset);
            DECODE_PRINTF("\n");
            TRACE_AND_STEP();
            *destreg32 = genop_long_operation[op1](*destreg32, srcval);
        } else {
            destreg = DECODE_RM_WORD_REGISTER(rh);
            DECODE_PRINTF(",");
            srcval = fetch_data_word(srcoffset);
            DECODE_PRINTF("\n");
            TRACE_AND_STEP();
            *destreg = genop_word_operation[op1](*destreg, srcval);
        }
    } else { /* register to register */
        if (M.x86.mode & SYSMODE_PREFIX_DATA) {
            var srcreg *u32
            destreg32 = DECODE_RM_LONG_REGISTER(rh);
            DECODE_PRINTF(",");
            srcreg = DECODE_RM_LONG_REGISTER(rl);
            DECODE_PRINTF("\n");
            TRACE_AND_STEP();
            *destreg32 = genop_long_operation[op1](*destreg32, *srcreg);
        } else {
            var srcreg *u16
            destreg = DECODE_RM_WORD_REGISTER(rh);
            DECODE_PRINTF(",");
            srcreg = DECODE_RM_WORD_REGISTER(rl);
            DECODE_PRINTF("\n");
            TRACE_AND_STEP();
            *destreg = genop_word_operation[op1](*destreg, *srcreg);
        }
    }
    DECODE_CLEAR_SEGOVR();
    END_OF_INSTR();
}

/****************************************************************************
REMARKS:
Handles opcodes 0x04, 0x0c, 0x14, 0x1c, 0x24, 0x2c, 0x34, 0x3c
****************************************************************************/
func x86emuOp_genop_byte_AL_IMM(u8 op1) {
    var srcval u8

    op1 = (op1 >> 3) & 0x7;

    START_OF_INSTR();
    DECODE_PRINTF(x86emu_GenOpName[op1]);
    DECODE_PRINTF("\tAL,");
    srcval = fetch_byte_imm();
    DECODE_PRINTF2("%x\n", srcval);
    TRACE_AND_STEP();
    M.x86.R_AL = genop_byte_operation[op1](M.x86.R_AL, srcval);
    DECODE_CLEAR_SEGOVR();
    END_OF_INSTR();
}

/****************************************************************************
REMARKS:
Handles opcodes 0x05, 0x0d, 0x15, 0x1d, 0x25, 0x2d, 0x35, 0x3d
****************************************************************************/
func x86emuOp_genop_word_AX_IMM(u8 op1) {
    var srcval u32

    op1 = (op1 >> 3) & 0x7;

    START_OF_INSTR();
    if (M.x86.mode & SYSMODE_PREFIX_DATA) {
        DECODE_PRINTF(x86emu_GenOpName[op1]);
        DECODE_PRINTF("\tEAX,");
        srcval = fetch_long_imm();
    } else {
        DECODE_PRINTF(x86emu_GenOpName[op1]);
        DECODE_PRINTF("\tAX,");
        srcval = fetch_word_imm();
    }
    DECODE_PRINTF2("%x\n", srcval);
    TRACE_AND_STEP();
    if (M.x86.mode & SYSMODE_PREFIX_DATA) {
        M.x86.R_EAX = genop_long_operation[op1](M.x86.R_EAX, srcval);
    } else {
        M.x86.R_AX = genop_word_operation[op1](M.x86.R_AX, (u16)srcval);
    }
    DECODE_CLEAR_SEGOVR();
    END_OF_INSTR();
}

/****************************************************************************
REMARKS:
Handles opcode 0x06
****************************************************************************/
func x86emuOp_push_ES(_ u8) {
    START_OF_INSTR();
    DECODE_PRINTF("PUSH\tES\n");
    TRACE_AND_STEP();
    push_word(M.x86.R_ES);
    DECODE_CLEAR_SEGOVR();
    END_OF_INSTR();
}

/****************************************************************************
REMARKS:
Handles opcode 0x07
****************************************************************************/
func x86emuOp_pop_ES(_ u8) {
    START_OF_INSTR();
    DECODE_PRINTF("POP\tES\n");
    TRACE_AND_STEP();
    M.x86.R_ES = pop_word();
    DECODE_CLEAR_SEGOVR();
    END_OF_INSTR();
}

/****************************************************************************
REMARKS:
Handles opcode 0x0e
****************************************************************************/
func x86emuOp_push_CS(_ u8) {
    START_OF_INSTR();
    DECODE_PRINTF("PUSH\tCS\n");
    TRACE_AND_STEP();
    push_word(M.x86.R_CS);
    DECODE_CLEAR_SEGOVR();
    END_OF_INSTR();
}

/****************************************************************************
REMARKS:
Handles opcode 0x0f. Escape for two-byte opcode (286 or better)
****************************************************************************/
func x86emuOp_two_byte(_ u8) {
    u8 op2 = sys_rdb(((u32)M.x86.R_CS << 4) + (M.x86.R_IP++));
    INC_DECODED_INST_LEN(1);
    (*x86emu_optab2[op2])(op2);
}

/****************************************************************************
REMARKS:
Handles opcode 0x16
****************************************************************************/
func x86emuOp_push_SS(_ u8) {
    START_OF_INSTR();
    DECODE_PRINTF("PUSH\tSS\n");
    TRACE_AND_STEP();
    push_word(M.x86.R_SS);
    DECODE_CLEAR_SEGOVR();
    END_OF_INSTR();
}

/****************************************************************************
REMARKS:
Handles opcode 0x17
****************************************************************************/
func x86emuOp_pop_SS(_ u8) {
    START_OF_INSTR();
    DECODE_PRINTF("POP\tSS\n");
    TRACE_AND_STEP();
    M.x86.R_SS = pop_word();
    DECODE_CLEAR_SEGOVR();
    END_OF_INSTR();
}

/****************************************************************************
REMARKS:
Handles opcode 0x1e
****************************************************************************/
func x86emuOp_push_DS(_ u8) {
    START_OF_INSTR();
    DECODE_PRINTF("PUSH\tDS\n");
    TRACE_AND_STEP();
    push_word(M.x86.R_DS);
    DECODE_CLEAR_SEGOVR();
    END_OF_INSTR();
}

/****************************************************************************
REMARKS:
Handles opcode 0x1f
****************************************************************************/
func x86emuOp_pop_DS(_ u8) {
    START_OF_INSTR();
    DECODE_PRINTF("POP\tDS\n");
    TRACE_AND_STEP();
    M.x86.R_DS = pop_word();
    DECODE_CLEAR_SEGOVR();
    END_OF_INSTR();
}

/****************************************************************************
REMARKS:
Handles opcode 0x26
****************************************************************************/
func x86emuOp_segovr_ES(_ u8) {
    START_OF_INSTR();
    DECODE_PRINTF("ES:\n");
    TRACE_AND_STEP();
    M.x86.mode |= SYSMODE_SEGOVR_ES;
    /*
     * note the lack of DECODE_CLEAR_SEGOVR(r) since, here is one of 4
     * opcode subroutines we do not want to do this.
     */
    END_OF_INSTR();
}

/****************************************************************************
REMARKS:
Handles opcode 0x27
****************************************************************************/
func x86emuOp_daa(_ u8) {
    START_OF_INSTR();
    DECODE_PRINTF("DAA\n");
    TRACE_AND_STEP();
    M.x86.R_AL = daa_byte(M.x86.R_AL);
    DECODE_CLEAR_SEGOVR();
    END_OF_INSTR();
}

/****************************************************************************
REMARKS:
Handles opcode 0x2e
****************************************************************************/
func x86emuOp_segovr_CS(_ u8) {
    START_OF_INSTR();
    DECODE_PRINTF("CS:\n");
    TRACE_AND_STEP();
    M.x86.mode |= SYSMODE_SEGOVR_CS;
    /* note no DECODE_CLEAR_SEGOVR here. */
    END_OF_INSTR();
}

/****************************************************************************
REMARKS:
Handles opcode 0x2f
****************************************************************************/
func x86emuOp_das(_ u8) {
    START_OF_INSTR();
    DECODE_PRINTF("DAS\n");
    TRACE_AND_STEP();
    M.x86.R_AL = das_byte(M.x86.R_AL);
    DECODE_CLEAR_SEGOVR();
    END_OF_INSTR();
}

/****************************************************************************
REMARKS:
Handles opcode 0x36
****************************************************************************/
func x86emuOp_segovr_SS(_ u8) {
    START_OF_INSTR();
    DECODE_PRINTF("SS:\n");
    TRACE_AND_STEP();
    M.x86.mode |= SYSMODE_SEGOVR_SS;
    /* no DECODE_CLEAR_SEGOVR ! */
    END_OF_INSTR();
}

/****************************************************************************
REMARKS:
Handles opcode 0x37
****************************************************************************/
func x86emuOp_aaa(_ u8) {
    START_OF_INSTR();
    DECODE_PRINTF("AAA\n");
    TRACE_AND_STEP();
    M.x86.R_AX = aaa_word(M.x86.R_AX);
    DECODE_CLEAR_SEGOVR();
    END_OF_INSTR();
}

/****************************************************************************
REMARKS:
Handles opcode 0x3e
****************************************************************************/
func x86emuOp_segovr_DS(_ u8) {
    START_OF_INSTR();
    DECODE_PRINTF("DS:\n");
    TRACE_AND_STEP();
    M.x86.mode |= SYSMODE_SEGOVR_DS;
    /* NO DECODE_CLEAR_SEGOVR! */
    END_OF_INSTR();
}

/****************************************************************************
REMARKS:
Handles opcode 0x3f
****************************************************************************/
func x86emuOp_aas(_ u8) {
    START_OF_INSTR();
    DECODE_PRINTF("AAS\n");
    TRACE_AND_STEP();
    M.x86.R_AX = aas_word(M.x86.R_AX);
    DECODE_CLEAR_SEGOVR();
    END_OF_INSTR();
}

/****************************************************************************
REMARKS:
Handles opcode 0x40 - 0x47
****************************************************************************/
func x86emuOp_inc_register(u8 op1) {
    START_OF_INSTR();
    op1 &= 0x7;
    DECODE_PRINTF("INC\t");
    if (M.x86.mode & SYSMODE_PREFIX_DATA) {
        u32 *reg;
        reg = DECODE_RM_LONG_REGISTER(op1);
        DECODE_PRINTF("\n");
        TRACE_AND_STEP();
        *reg = inc_long(*reg);
    } else {
        u16 *reg;
        reg = DECODE_RM_WORD_REGISTER(op1);
        DECODE_PRINTF("\n");
        TRACE_AND_STEP();
        *reg = inc_word(*reg);
    }
    DECODE_CLEAR_SEGOVR();
    END_OF_INSTR();
}

/****************************************************************************
REMARKS:
Handles opcode 0x48 - 0x4F
****************************************************************************/
func x86emuOp_dec_register(u8 op1) {
    START_OF_INSTR();
    op1 &= 0x7;
    DECODE_PRINTF("DEC\t");
    if (M.x86.mode & SYSMODE_PREFIX_DATA) {
        u32 *reg;
        reg = DECODE_RM_LONG_REGISTER(op1);
        DECODE_PRINTF("\n");
        TRACE_AND_STEP();
        *reg = dec_long(*reg);
    } else {
        u16 *reg;
        reg = DECODE_RM_WORD_REGISTER(op1);
        DECODE_PRINTF("\n");
        TRACE_AND_STEP();
        *reg = dec_word(*reg);
    }
    DECODE_CLEAR_SEGOVR();
    END_OF_INSTR();
}

/****************************************************************************
REMARKS:
Handles opcode 0x50 - 0x57
****************************************************************************/
func x86emuOp_push_register(u8 op1) {
    START_OF_INSTR();
    op1 &= 0x7;
    DECODE_PRINTF("PUSH\t");
    if (M.x86.mode & SYSMODE_PREFIX_DATA) {
        u32 *reg;
        reg = DECODE_RM_LONG_REGISTER(op1);
        DECODE_PRINTF("\n");
        TRACE_AND_STEP();
        push_long(*reg);
    } else {
        u16 *reg;
        reg = DECODE_RM_WORD_REGISTER(op1);
        DECODE_PRINTF("\n");
        TRACE_AND_STEP();
        push_word(*reg);
    }
    DECODE_CLEAR_SEGOVR();
    END_OF_INSTR();
}

/****************************************************************************
REMARKS:
Handles opcode 0x58 - 0x5F
****************************************************************************/
func x86emuOp_pop_register(u8 op1) {
    START_OF_INSTR();
    op1 &= 0x7;
    DECODE_PRINTF("POP\t");
    if (M.x86.mode & SYSMODE_PREFIX_DATA) {
        u32 *reg;
        reg = DECODE_RM_LONG_REGISTER(op1);
        DECODE_PRINTF("\n");
        TRACE_AND_STEP();
        *reg = pop_long();
    } else {
        u16 *reg;
        reg = DECODE_RM_WORD_REGISTER(op1);
        DECODE_PRINTF("\n");
        TRACE_AND_STEP();
        *reg = pop_word();
    }
    DECODE_CLEAR_SEGOVR();
    END_OF_INSTR();
}

/****************************************************************************
REMARKS:
Handles opcode 0x60
****************************************************************************/
func x86emuOp_push_all(_ u8) {
    START_OF_INSTR();
    if (M.x86.mode & SYSMODE_PREFIX_DATA) {
        DECODE_PRINTF("PUSHAD\n");
    } else {
        DECODE_PRINTF("PUSHA\n");
    }
    TRACE_AND_STEP();
    if (M.x86.mode & SYSMODE_PREFIX_DATA) {
        u32 old_sp = M.x86.R_ESP;

        push_long(M.x86.R_EAX);
        push_long(M.x86.R_ECX);
        push_long(M.x86.R_EDX);
        push_long(M.x86.R_EBX);
        push_long(old_sp);
        push_long(M.x86.R_EBP);
        push_long(M.x86.R_ESI);
        push_long(M.x86.R_EDI);
    } else {
        u16 old_sp = M.x86.R_SP;

        push_word(M.x86.R_AX);
        push_word(M.x86.R_CX);
        push_word(M.x86.R_DX);
        push_word(M.x86.R_BX);
        push_word(old_sp);
        push_word(M.x86.R_BP);
        push_word(M.x86.R_SI);
        push_word(M.x86.R_DI);
    }
    DECODE_CLEAR_SEGOVR();
    END_OF_INSTR();
}

/****************************************************************************
REMARKS:
Handles opcode 0x61
****************************************************************************/
func x86emuOp_pop_all(_ u8) {
    START_OF_INSTR();
    if (M.x86.mode & SYSMODE_PREFIX_DATA) {
        DECODE_PRINTF("POPAD\n");
    } else {
        DECODE_PRINTF("POPA\n");
    }
    TRACE_AND_STEP();
    if (M.x86.mode & SYSMODE_PREFIX_DATA) {
        M.x86.R_EDI = pop_long();
        M.x86.R_ESI = pop_long();
        M.x86.R_EBP = pop_long();
        M.x86.R_ESP += 4; /* skip ESP */
        M.x86.R_EBX = pop_long();
        M.x86.R_EDX = pop_long();
        M.x86.R_ECX = pop_long();
        M.x86.R_EAX = pop_long();
    } else {
        M.x86.R_DI = pop_word();
        M.x86.R_SI = pop_word();
        M.x86.R_BP = pop_word();
        M.x86.R_SP += 2; /* skip SP */
        M.x86.R_BX = pop_word();
        M.x86.R_DX = pop_word();
        M.x86.R_CX = pop_word();
        M.x86.R_AX = pop_word();
    }
    DECODE_CLEAR_SEGOVR();
    END_OF_INSTR();
}

/*opcode 0x62   ILLEGAL OP, calls x86emuOp_illegal_op() */
/*opcode 0x63   ILLEGAL OP, calls x86emuOp_illegal_op() */

/****************************************************************************
REMARKS:
Handles opcode 0x64
****************************************************************************/
func x86emuOp_segovr_FS(_ u8) {
    START_OF_INSTR();
    DECODE_PRINTF("FS:\n");
    TRACE_AND_STEP();
    M.x86.mode |= SYSMODE_SEGOVR_FS;
    /*
     * note the lack of DECODE_CLEAR_SEGOVR(r) since, here is one of 4
     * opcode subroutines we do not want to do this.
     */
    END_OF_INSTR();
}

/****************************************************************************
REMARKS:
Handles opcode 0x65
****************************************************************************/
func x86emuOp_segovr_GS(_ u8) {
    START_OF_INSTR();
    DECODE_PRINTF("GS:\n");
    TRACE_AND_STEP();
    M.x86.mode |= SYSMODE_SEGOVR_GS;
    /*
     * note the lack of DECODE_CLEAR_SEGOVR(r) since, here is one of 4
     * opcode subroutines we do not want to do this.
     */
    END_OF_INSTR();
}

/****************************************************************************
REMARKS:
Handles opcode 0x66 - prefix for 32-bit register
****************************************************************************/
func x86emuOp_prefix_data(_ u8) {
    START_OF_INSTR();
    DECODE_PRINTF("DATA:\n");
    TRACE_AND_STEP();
    M.x86.mode |= SYSMODE_PREFIX_DATA;
    /* note no DECODE_CLEAR_SEGOVR here. */
    END_OF_INSTR();
}

/****************************************************************************
REMARKS:
Handles opcode 0x67 - prefix for 32-bit address
****************************************************************************/
func x86emuOp_prefix_addr(_ u8) {
    START_OF_INSTR();
    DECODE_PRINTF("ADDR:\n");
    TRACE_AND_STEP();
    M.x86.mode |= SYSMODE_PREFIX_ADDR;
    /* note no DECODE_CLEAR_SEGOVR here. */
    END_OF_INSTR();
}

/****************************************************************************
REMARKS:
Handles opcode 0x68
****************************************************************************/
func x86emuOp_push_word_IMM(_ u8) {
    u32 imm;

    START_OF_INSTR();
    if (M.x86.mode & SYSMODE_PREFIX_DATA) {
        imm = fetch_long_imm();
    } else {
        imm = fetch_word_imm();
    }
    DECODE_PRINTF2("PUSH\t%x\n", imm);
    TRACE_AND_STEP();
    if (M.x86.mode & SYSMODE_PREFIX_DATA) {
        push_long(imm);
    } else {
        push_word((u16)imm);
    }
    DECODE_CLEAR_SEGOVR();
    END_OF_INSTR();
}

/****************************************************************************
REMARKS:
Handles opcode 0x69
****************************************************************************/
func x86emuOp_imul_word_IMM(_ u8) {
    var mod, rl, rh int
    var srcoffset uint

    START_OF_INSTR();
    DECODE_PRINTF("IMUL\t");
    FETCH_DECODE_MODRM(mod, rh, rl);
    if (mod < 3) {
        srcoffset = decode_rmXX_address(mod, rl);
        if (M.x86.mode & SYSMODE_PREFIX_DATA) {
            u32 *destreg;
            var srcval u32
            u32 res_lo,res_hi;
            s32 imm;

            destreg = DECODE_RM_LONG_REGISTER(rh);
            DECODE_PRINTF(",");
            srcval = fetch_data_long(srcoffset);
            imm = fetch_long_imm();
            DECODE_PRINTF2(",%d\n", (s32)imm);
            TRACE_AND_STEP();
            imul_long_direct(&res_lo,&res_hi,(s32)srcval,(s32)imm);
            if ((((res_lo & 0x80000000) == 0) && (res_hi == 0x00000000)) ||
                (((res_lo & 0x80000000) != 0) && (res_hi == 0xFFFFFFFF))) {
                CLEAR_FLAG(F_CF);
                CLEAR_FLAG(F_OF);
            } else {
                SET_FLAG(F_CF);
                SET_FLAG(F_OF);
            }
            *destreg = (u32)res_lo;
        } else {
            var destreg *u16
            u16 srcval;
            u32 res;
            s16 imm;

            destreg = DECODE_RM_WORD_REGISTER(rh);
            DECODE_PRINTF(",");
            srcval = fetch_data_word(srcoffset);
            imm = fetch_word_imm();
            DECODE_PRINTF2(",%d\n", (s32)imm);
            TRACE_AND_STEP();
            res = (s16)srcval * (s16)imm;
            if ((((res & 0x8000) == 0) && ((res >> 16) == 0x0000)) ||
                (((res & 0x8000) != 0) && ((res >> 16) == 0xFFFF))) {
                CLEAR_FLAG(F_CF);
                CLEAR_FLAG(F_OF);
            } else {
                SET_FLAG(F_CF);
                SET_FLAG(F_OF);
            }
            *destreg = (u16)res;
        }
    } else { /* register to register */
        if (M.x86.mode & SYSMODE_PREFIX_DATA) {
            u32 *destreg,*srcreg;
            u32 res_lo,res_hi;
            s32 imm;

            destreg = DECODE_RM_LONG_REGISTER(rh);
            DECODE_PRINTF(",");
            srcreg = DECODE_RM_LONG_REGISTER(rl);
            imm = fetch_long_imm();
            DECODE_PRINTF2(",%d\n", (s32)imm);
            TRACE_AND_STEP();
            imul_long_direct(&res_lo,&res_hi,(s32)*srcreg,(s32)imm);
            if ((((res_lo & 0x80000000) == 0) && (res_hi == 0x00000000)) ||
                (((res_lo & 0x80000000) != 0) && (res_hi == 0xFFFFFFFF))) {
                CLEAR_FLAG(F_CF);
                CLEAR_FLAG(F_OF);
            } else {
                SET_FLAG(F_CF);
                SET_FLAG(F_OF);
            }
            *destreg = (u32)res_lo;
        } else {
            u16 *destreg,*srcreg;
            u32 res;
            s16 imm;

            destreg = DECODE_RM_WORD_REGISTER(rh);
            DECODE_PRINTF(",");
            srcreg = DECODE_RM_WORD_REGISTER(rl);
            imm = fetch_word_imm();
            DECODE_PRINTF2(",%d\n", (s32)imm);
            res = (s16)*srcreg * (s16)imm;
            if ((((res & 0x8000) == 0) && ((res >> 16) == 0x0000)) ||
                (((res & 0x8000) != 0) && ((res >> 16) == 0xFFFF))) {
                CLEAR_FLAG(F_CF);
                CLEAR_FLAG(F_OF);
            } else {
                SET_FLAG(F_CF);
                SET_FLAG(F_OF);
            }
            *destreg = (u16)res;
        }
    }
    DECODE_CLEAR_SEGOVR();
    END_OF_INSTR();
}

/****************************************************************************
REMARKS:
Handles opcode 0x6a
****************************************************************************/
func x86emuOp_push_byte_IMM(_ u8) {
    s16 imm;

    START_OF_INSTR();
    imm = (s8)fetch_byte_imm();
    DECODE_PRINTF2("PUSH\t%d\n", imm);
    TRACE_AND_STEP();
    if (M.x86.mode & SYSMODE_PREFIX_DATA) {
        push_long(imm);
    } else {
        push_word(imm);
    }
    DECODE_CLEAR_SEGOVR();
    END_OF_INSTR();
}

/****************************************************************************
REMARKS:
Handles opcode 0x6b
****************************************************************************/
func x86emuOp_imul_byte_IMM(_ u8) {
    var mod, rl, rh int
    var srcoffset uint
    s8 imm;

    START_OF_INSTR();
    DECODE_PRINTF("IMUL\t");
    FETCH_DECODE_MODRM(mod, rh, rl);
    if (mod < 3) {
        srcoffset = decode_rmXX_address(mod, rl);
        if (M.x86.mode & SYSMODE_PREFIX_DATA) {
            u32 *destreg;
            var srcval u32
            u32 res_lo,res_hi;

            destreg = DECODE_RM_LONG_REGISTER(rh);
            DECODE_PRINTF(",");
            srcval = fetch_data_long(srcoffset);
            imm = fetch_byte_imm();
            DECODE_PRINTF2(",%d\n", (s32)imm);
            TRACE_AND_STEP();
            imul_long_direct(&res_lo,&res_hi,(s32)srcval,(s32)imm);
            if ((((res_lo & 0x80000000) == 0) && (res_hi == 0x00000000)) ||
                (((res_lo & 0x80000000) != 0) && (res_hi == 0xFFFFFFFF))) {
                CLEAR_FLAG(F_CF);
                CLEAR_FLAG(F_OF);
            } else {
                SET_FLAG(F_CF);
                SET_FLAG(F_OF);
            }
            *destreg = (u32)res_lo;
        } else {
            var destreg *u16
            u16 srcval;
            u32 res;

            destreg = DECODE_RM_WORD_REGISTER(rh);
            DECODE_PRINTF(",");
            srcval = fetch_data_word(srcoffset);
            imm = fetch_byte_imm();
            DECODE_PRINTF2(",%d\n", (s32)imm);
            TRACE_AND_STEP();
            res = (s16)srcval * (s16)imm;
            if ((((res & 0x8000) == 0) && ((res >> 16) == 0x0000)) ||
                (((res & 0x8000) != 0) && ((res >> 16) == 0xFFFF))) {
                CLEAR_FLAG(F_CF);
                CLEAR_FLAG(F_OF);
            } else {
                SET_FLAG(F_CF);
                SET_FLAG(F_OF);
            }
            *destreg = (u16)res;
        }
    } else { /* register to register */
        if (M.x86.mode & SYSMODE_PREFIX_DATA) {
            u32 *destreg,*srcreg;
            u32 res_lo,res_hi;

            destreg = DECODE_RM_LONG_REGISTER(rh);
            DECODE_PRINTF(",");
            srcreg = DECODE_RM_LONG_REGISTER(rl);
            imm = fetch_byte_imm();
            DECODE_PRINTF2(",%d\n", (s32)imm);
            TRACE_AND_STEP();
            imul_long_direct(&res_lo,&res_hi,(s32)*srcreg,(s32)imm);
            if ((((res_lo & 0x80000000) == 0) && (res_hi == 0x00000000)) ||
                (((res_lo & 0x80000000) != 0) && (res_hi == 0xFFFFFFFF))) {
                CLEAR_FLAG(F_CF);
                CLEAR_FLAG(F_OF);
            } else {
                SET_FLAG(F_CF);
                SET_FLAG(F_OF);
            }
            *destreg = (u32)res_lo;
        } else {
            u16 *destreg,*srcreg;
            u32 res;

            destreg = DECODE_RM_WORD_REGISTER(rh);
            DECODE_PRINTF(",");
            srcreg = DECODE_RM_WORD_REGISTER(rl);
            imm = fetch_byte_imm();
            DECODE_PRINTF2(",%d\n", (s32)imm);
            TRACE_AND_STEP();
            res = (s16)*srcreg * (s16)imm;
            if ((((res & 0x8000) == 0) && ((res >> 16) == 0x0000)) ||
                (((res & 0x8000) != 0) && ((res >> 16) == 0xFFFF))) {
                CLEAR_FLAG(F_CF);
                CLEAR_FLAG(F_OF);
            } else {
                SET_FLAG(F_CF);
                SET_FLAG(F_OF);
            }
            *destreg = (u16)res;
        }
    }
    DECODE_CLEAR_SEGOVR();
    END_OF_INSTR();
}

/****************************************************************************
REMARKS:
Handles opcode 0x6c
****************************************************************************/
func x86emuOp_ins_byte(_ u8) {
    START_OF_INSTR();
    DECODE_PRINTF("INSB\n");
    ins(1);
    TRACE_AND_STEP();
    DECODE_CLEAR_SEGOVR();
    END_OF_INSTR();
}

/****************************************************************************
REMARKS:
Handles opcode 0x6d
****************************************************************************/
func x86emuOp_ins_word(_ u8) {
    START_OF_INSTR();
    if (M.x86.mode & SYSMODE_PREFIX_DATA) {
        DECODE_PRINTF("INSD\n");
        ins(4);
    } else {
        DECODE_PRINTF("INSW\n");
        ins(2);
    }
    TRACE_AND_STEP();
    DECODE_CLEAR_SEGOVR();
    END_OF_INSTR();
}

/****************************************************************************
REMARKS:
Handles opcode 0x6e
****************************************************************************/
func x86emuOp_outs_byte(_ u8) {
    START_OF_INSTR();
    DECODE_PRINTF("OUTSB\n");
    outs(1);
    TRACE_AND_STEP();
    DECODE_CLEAR_SEGOVR();
    END_OF_INSTR();
}

/****************************************************************************
REMARKS:
Handles opcode 0x6f
****************************************************************************/
func x86emuOp_outs_word(_ u8) {
    START_OF_INSTR();
    if (M.x86.mode & SYSMODE_PREFIX_DATA) {
        DECODE_PRINTF("OUTSD\n");
        outs(4);
    } else {
        DECODE_PRINTF("OUTSW\n");
        outs(2);
    }
    TRACE_AND_STEP();
    DECODE_CLEAR_SEGOVR();
    END_OF_INSTR();
}

/****************************************************************************
REMARKS:
Handles opcode 0x70 - 0x7F
****************************************************************************/
func x86emuOp_jump_near_cond(u8 op1) {
    s8 offset;
    u16 target;
    int cond;

    /* jump to byte offset if overflow flag is set */
    START_OF_INSTR();
    cond = x86emu_check_jump_condition(op1 & 0xF);
    offset = (s8)fetch_byte_imm();
    target = (u16)(M.x86.R_IP + (s16)offset);
    DECODE_PRINTF2("%x\n", target);
    TRACE_AND_STEP();
    if (cond) {
        M.x86.R_IP = target;
 JMP_TRACE(M.x86.saved_cs, M.x86.saved_ip, M.x86.R_CS, M.x86.R_IP, " NEAR COND ");
    }
    DECODE_CLEAR_SEGOVR();
    END_OF_INSTR();
}

/****************************************************************************
REMARKS:
Handles opcode 0x80
****************************************************************************/
func x86emuOp_opc80_byte_RM_IMM(_ u8) {
    var mod, rl, rh int
    u8 *destreg;
    var destoffset uint
    u8 imm;
    var destval u8

    /*
     * Weirdo special case instruction format.  Part of the opcode
     * held below in "RH".  Doubly nested case would result, except
     * that the decoded instruction
     */
    START_OF_INSTR();
    FETCH_DECODE_MODRM(mod, rh, rl);

    if (DEBUG_DECODE()) {
        /* XXX DECODE_PRINTF may be changed to something more
           general, so that it is important to leave the strings
           in the same format, even though the result is that the
           above test is done twice. */

        switch (rh) {
        case 0:
            DECODE_PRINTF("ADD\t");
            break;
        case 1:
            DECODE_PRINTF("OR\t");
            break;
        case 2:
            DECODE_PRINTF("ADC\t");
            break;
        case 3:
            DECODE_PRINTF("SBB\t");
            break;
        case 4:
            DECODE_PRINTF("AND\t");
            break;
        case 5:
            DECODE_PRINTF("SUB\t");
            break;
        case 6:
            DECODE_PRINTF("XOR\t");
            break;
        case 7:
            DECODE_PRINTF("CMP\t");
            break;
        }
    }

    /* know operation, decode the mod byte to find the addressing
       mode. */
    if (mod < 3) {
        DECODE_PRINTF("BYTE PTR ");
        destoffset = decode_rmXX_address(mod, rl);
        DECODE_PRINTF(",");
        destval = fetch_data_byte(destoffset);
        imm = fetch_byte_imm();
        DECODE_PRINTF2("%x\n", imm);
        TRACE_AND_STEP();
        destval = (*genop_byte_operation[rh]) (destval, imm);
        if (rh != 7)
            store_data_byte(destoffset, destval);
    } else { /* register to register */
        destreg = DECODE_RM_BYTE_REGISTER(rl);
        DECODE_PRINTF(",");
        imm = fetch_byte_imm();
        DECODE_PRINTF2("%x\n", imm);
        TRACE_AND_STEP();
        *destreg = (*genop_byte_operation[rh]) (*destreg, imm);
    }
    DECODE_CLEAR_SEGOVR();
    END_OF_INSTR();
}

/****************************************************************************
REMARKS:
Handles opcode 0x81
****************************************************************************/
func x86emuOp_opc81_word_RM_IMM(_ u8) {
    var mod, rl, rh int
    var destoffset uint

    /*
     * Weirdo special case instruction format.  Part of the opcode
     * held below in "RH".  Doubly nested case would result, except
     * that the decoded instruction
     */
    START_OF_INSTR();
    FETCH_DECODE_MODRM(mod, rh, rl);

    if (DEBUG_DECODE()) {
        /* XXX DECODE_PRINTF may be changed to something more
           general, so that it is important to leave the strings
           in the same format, even though the result is that the
           above test is done twice. */

        switch (rh) {
        case 0:
            DECODE_PRINTF("ADD\t");
            break;
        case 1:
            DECODE_PRINTF("OR\t");
            break;
        case 2:
            DECODE_PRINTF("ADC\t");
            break;
        case 3:
            DECODE_PRINTF("SBB\t");
            break;
        case 4:
            DECODE_PRINTF("AND\t");
            break;
        case 5:
            DECODE_PRINTF("SUB\t");
            break;
        case 6:
            DECODE_PRINTF("XOR\t");
            break;
        case 7:
            DECODE_PRINTF("CMP\t");
            break;
        }
    }

    /*
     * Know operation, decode the mod byte to find the addressing
     * mode.
     */
    if (mod < 3) {
        DECODE_PRINTF("DWORD PTR ");
        destoffset = decode_rmXX_address(mod, rl);
        if (M.x86.mode & SYSMODE_PREFIX_DATA) {
            u32 destval,imm;

            DECODE_PRINTF(",");
            destval = fetch_data_long(destoffset);
            imm = fetch_long_imm();
            DECODE_PRINTF2("%x\n", imm);
            TRACE_AND_STEP();
            destval = (*genop_long_operation[rh]) (destval, imm);
            if (rh != 7)
                store_data_long(destoffset, destval);
        } else {
            u16 destval,imm;

            DECODE_PRINTF(",");
            destval = fetch_data_word(destoffset);
            imm = fetch_word_imm();
            DECODE_PRINTF2("%x\n", imm);
            TRACE_AND_STEP();
            destval = (*genop_word_operation[rh]) (destval, imm);
            if (rh != 7)
                store_data_word(destoffset, destval);
        }
    } else { /* register to register */
        if (M.x86.mode & SYSMODE_PREFIX_DATA) {
            u32 *destreg, imm;

            destreg = DECODE_RM_LONG_REGISTER(rl);
            DECODE_PRINTF(",");
            imm = fetch_long_imm();
            DECODE_PRINTF2("%x\n", imm);
            TRACE_AND_STEP();
            *destreg = (*genop_long_operation[rh]) (*destreg, imm);
        } else {
            u16 *destreg, imm;

            destreg = DECODE_RM_WORD_REGISTER(rl);
            DECODE_PRINTF(",");
            imm = fetch_word_imm();
            DECODE_PRINTF2("%x\n", imm);
            TRACE_AND_STEP();
            *destreg = (*genop_word_operation[rh]) (*destreg, imm);
        }
    }
    DECODE_CLEAR_SEGOVR();
    END_OF_INSTR();
}

/****************************************************************************
REMARKS:
Handles opcode 0x82
****************************************************************************/
func x86emuOp_opc82_byte_RM_IMM(_ u8) {
    var mod, rl, rh int
    u8 *destreg;
    var destoffset uint
    u8 imm;
    var destval u8

    /*
     * Weirdo special case instruction format.  Part of the opcode
     * held below in "RH".  Doubly nested case would result, except
     * that the decoded instruction Similar to opcode 81, except that
     * the immediate byte is sign extended to a word length.
     */
    START_OF_INSTR();
    FETCH_DECODE_MODRM(mod, rh, rl);

    if (DEBUG_DECODE()) {
        /* XXX DECODE_PRINTF may be changed to something more
           general, so that it is important to leave the strings
           in the same format, even though the result is that the
           above test is done twice. */
        switch (rh) {
        case 0:
            DECODE_PRINTF("ADD\t");
            break;
        case 1:
            DECODE_PRINTF("OR\t");
            break;
        case 2:
            DECODE_PRINTF("ADC\t");
            break;
        case 3:
            DECODE_PRINTF("SBB\t");
            break;
        case 4:
            DECODE_PRINTF("AND\t");
            break;
        case 5:
            DECODE_PRINTF("SUB\t");
            break;
        case 6:
            DECODE_PRINTF("XOR\t");
            break;
        case 7:
            DECODE_PRINTF("CMP\t");
            break;
        }
    }

    /* know operation, decode the mod byte to find the addressing
       mode. */
    if (mod < 3) {
        DECODE_PRINTF("BYTE PTR ");
        destoffset = decode_rmXX_address(mod, rl);
        destval = fetch_data_byte(destoffset);
        imm = fetch_byte_imm();
        DECODE_PRINTF2(",%x\n", imm);
        TRACE_AND_STEP();
        destval = (*genop_byte_operation[rh]) (destval, imm);
        if (rh != 7)
            store_data_byte(destoffset, destval);
    } else { /* register to register */
        destreg = DECODE_RM_BYTE_REGISTER(rl);
        imm = fetch_byte_imm();
        DECODE_PRINTF2(",%x\n", imm);
        TRACE_AND_STEP();
        *destreg = (*genop_byte_operation[rh]) (*destreg, imm);
    }
    DECODE_CLEAR_SEGOVR();
    END_OF_INSTR();
}

/****************************************************************************
REMARKS:
Handles opcode 0x83
****************************************************************************/
func x86emuOp_opc83_word_RM_IMM(_ u8) {
    var mod, rl, rh int
    var destoffset uint

    /*
     * Weirdo special case instruction format.  Part of the opcode
     * held below in "RH".  Doubly nested case would result, except
     * that the decoded instruction Similar to opcode 81, except that
     * the immediate byte is sign extended to a word length.
     */
    START_OF_INSTR();
    FETCH_DECODE_MODRM(mod, rh, rl);

    if (DEBUG_DECODE()) {
        /* XXX DECODE_PRINTF may be changed to something more
           general, so that it is important to leave the strings
           in the same format, even though the result is that the
           above test is done twice. */
       switch (rh) {
        case 0:
            DECODE_PRINTF("ADD\t");
            break;
        case 1:
            DECODE_PRINTF("OR\t");
            break;
        case 2:
            DECODE_PRINTF("ADC\t");
            break;
        case 3:
            DECODE_PRINTF("SBB\t");
            break;
        case 4:
            DECODE_PRINTF("AND\t");
            break;
        case 5:
            DECODE_PRINTF("SUB\t");
            break;
        case 6:
            DECODE_PRINTF("XOR\t");
            break;
        case 7:
            DECODE_PRINTF("CMP\t");
            break;
        }
    }

    /* know operation, decode the mod byte to find the addressing
       mode. */
    if (mod < 3) {
        DECODE_PRINTF("DWORD PTR ");
        destoffset = decode_rmXX_address(mod,rl);

        if (M.x86.mode & SYSMODE_PREFIX_DATA) {
            u32 destval,imm;

            destval = fetch_data_long(destoffset);
            imm = (s8) fetch_byte_imm();
            DECODE_PRINTF2(",%x\n", imm);
            TRACE_AND_STEP();
            destval = (*genop_long_operation[rh]) (destval, imm);
            if (rh != 7)
                store_data_long(destoffset, destval);
        } else {
            u16 destval,imm;

            destval = fetch_data_word(destoffset);
            imm = (s8) fetch_byte_imm();
            DECODE_PRINTF2(",%x\n", imm);
            TRACE_AND_STEP();
            destval = (*genop_word_operation[rh]) (destval, imm);
            if (rh != 7)
                store_data_word(destoffset, destval);
        }
    } else { /* register to register */
        if (M.x86.mode & SYSMODE_PREFIX_DATA) {
            u32 *destreg, imm;

            destreg = DECODE_RM_LONG_REGISTER(rl);
            imm = (s8) fetch_byte_imm();
            DECODE_PRINTF2(",%x\n", imm);
            TRACE_AND_STEP();
            *destreg = (*genop_long_operation[rh]) (*destreg, imm);
        } else {
            u16 *destreg, imm;

            destreg = DECODE_RM_WORD_REGISTER(rl);
            imm = (s8) fetch_byte_imm();
            DECODE_PRINTF2(",%x\n", imm);
            TRACE_AND_STEP();
            *destreg = (*genop_word_operation[rh]) (*destreg, imm);
        }
    }
    DECODE_CLEAR_SEGOVR();
    END_OF_INSTR();
}

/****************************************************************************
REMARKS:
Handles opcode 0x84
****************************************************************************/
func x86emuOp_test_byte_RM_R(_ u8) {
    var mod, rl, rh int
    var destreg, srcreg *u8
    var destoffset uint
    var destval u8

    START_OF_INSTR();
    DECODE_PRINTF("TEST\t");
    FETCH_DECODE_MODRM(mod, rh, rl);
    if (mod < 3) {
        destoffset = decode_rmXX_address(mod, rl);
        DECODE_PRINTF(",");
        destval = fetch_data_byte(destoffset);
        srcreg = DECODE_RM_BYTE_REGISTER(rh);
        DECODE_PRINTF("\n");
        TRACE_AND_STEP();
        test_byte(destval, *srcreg);
    } else { /* register to register */
        destreg = DECODE_RM_BYTE_REGISTER(rl);
        DECODE_PRINTF(",");
        srcreg = DECODE_RM_BYTE_REGISTER(rh);
        DECODE_PRINTF("\n");
        TRACE_AND_STEP();
        test_byte(*destreg, *srcreg);
    }
    DECODE_CLEAR_SEGOVR();
    END_OF_INSTR();
}

/****************************************************************************
REMARKS:
Handles opcode 0x85
****************************************************************************/
func x86emuOp_test_word_RM_R(_ u8) {
    var mod, rl, rh int
    var destoffset uint

    START_OF_INSTR();
    DECODE_PRINTF("TEST\t");
    FETCH_DECODE_MODRM(mod, rh, rl);
    if (mod < 3) {
        destoffset = decode_rmXX_address(mod, rl);
        if (M.x86.mode & SYSMODE_PREFIX_DATA) {
            var destval u32
            var srcreg *u32

            DECODE_PRINTF(",");
            destval = fetch_data_long(destoffset);
            srcreg = DECODE_RM_LONG_REGISTER(rh);
            DECODE_PRINTF("\n");
            TRACE_AND_STEP();
            test_long(destval, *srcreg);
        } else {
            var destval u16
            var srcreg *u16

            DECODE_PRINTF(",");
            destval = fetch_data_word(destoffset);
            srcreg = DECODE_RM_WORD_REGISTER(rh);
            DECODE_PRINTF("\n");
            TRACE_AND_STEP();
            test_word(destval, *srcreg);
        }
    } else { /* register to register */
        if (M.x86.mode & SYSMODE_PREFIX_DATA) {
            u32 *destreg,*srcreg;

            destreg = DECODE_RM_LONG_REGISTER(rl);
            DECODE_PRINTF(",");
            srcreg = DECODE_RM_LONG_REGISTER(rh);
            DECODE_PRINTF("\n");
            TRACE_AND_STEP();
            test_long(*destreg, *srcreg);
        } else {
            u16 *destreg,*srcreg;

            destreg = DECODE_RM_WORD_REGISTER(rl);
            DECODE_PRINTF(",");
            srcreg = DECODE_RM_WORD_REGISTER(rh);
            DECODE_PRINTF("\n");
            TRACE_AND_STEP();
            test_word(*destreg, *srcreg);
        }
    }
    DECODE_CLEAR_SEGOVR();
    END_OF_INSTR();
}

/****************************************************************************
REMARKS:
Handles opcode 0x86
****************************************************************************/
func x86emuOp_xchg_byte_RM_R(_ u8) {
    var mod, rl, rh int
    var destreg, srcreg *u8
    var destoffset uint
    var destval u8
    u8 tmp;

    START_OF_INSTR();
    DECODE_PRINTF("XCHG\t");
    FETCH_DECODE_MODRM(mod, rh, rl);
    if (mod < 3) {
        destoffset = decode_rmXX_address(mod, rl);
        DECODE_PRINTF(",");
        destval = fetch_data_byte(destoffset);
        srcreg = DECODE_RM_BYTE_REGISTER(rh);
        DECODE_PRINTF("\n");
        TRACE_AND_STEP();
        tmp = *srcreg;
        *srcreg = destval;
        destval = tmp;
        store_data_byte(destoffset, destval);
    } else { /* register to register */
        destreg = DECODE_RM_BYTE_REGISTER(rl);
        DECODE_PRINTF(",");
        srcreg = DECODE_RM_BYTE_REGISTER(rh);
        DECODE_PRINTF("\n");
        TRACE_AND_STEP();
        tmp = *srcreg;
        *srcreg = *destreg;
        *destreg = tmp;
    }
    DECODE_CLEAR_SEGOVR();
    END_OF_INSTR();
}

/****************************************************************************
REMARKS:
Handles opcode 0x87
****************************************************************************/
func x86emuOp_xchg_word_RM_R(_ u8) {
    var mod, rl, rh int
    var destoffset uint

    START_OF_INSTR();
    DECODE_PRINTF("XCHG\t");
    FETCH_DECODE_MODRM(mod, rh, rl);
    if (mod < 3) {
        destoffset = decode_rmXX_address(mod, rl);
        DECODE_PRINTF(",");
        if (M.x86.mode & SYSMODE_PREFIX_DATA) {
            var srcreg *u32
            u32 destval,tmp;

            destval = fetch_data_long(destoffset);
            srcreg = DECODE_RM_LONG_REGISTER(rh);
            DECODE_PRINTF("\n");
            TRACE_AND_STEP();
            tmp = *srcreg;
            *srcreg = destval;
            destval = tmp;
            store_data_long(destoffset, destval);
        } else {
            var srcreg *u16
            u16 destval,tmp;

            destval = fetch_data_word(destoffset);
            srcreg = DECODE_RM_WORD_REGISTER(rh);
            DECODE_PRINTF("\n");
            TRACE_AND_STEP();
            tmp = *srcreg;
            *srcreg = destval;
            destval = tmp;
            store_data_word(destoffset, destval);
        }
    } else { /* register to register */
        if (M.x86.mode & SYSMODE_PREFIX_DATA) {
            u32 *destreg,*srcreg;
            u32 tmp;

            destreg = DECODE_RM_LONG_REGISTER(rl);
            DECODE_PRINTF(",");
            srcreg = DECODE_RM_LONG_REGISTER(rh);
            DECODE_PRINTF("\n");
            TRACE_AND_STEP();
            tmp = *srcreg;
            *srcreg = *destreg;
            *destreg = tmp;
        } else {
            u16 *destreg,*srcreg;
            u16 tmp;

            destreg = DECODE_RM_WORD_REGISTER(rl);
            DECODE_PRINTF(",");
            srcreg = DECODE_RM_WORD_REGISTER(rh);
            DECODE_PRINTF("\n");
            TRACE_AND_STEP();
            tmp = *srcreg;
            *srcreg = *destreg;
            *destreg = tmp;
        }
    }
    DECODE_CLEAR_SEGOVR();
    END_OF_INSTR();
}

/****************************************************************************
REMARKS:
Handles opcode 0x88
****************************************************************************/
func x86emuOp_mov_byte_RM_R(_ u8) {
    var mod, rl, rh int
    var destreg, srcreg *u8
    var destoffset uint

    START_OF_INSTR();
    DECODE_PRINTF("MOV\t");
    FETCH_DECODE_MODRM(mod, rh, rl);
    if (mod < 3) {
        destoffset = decode_rmXX_address(mod, rl);
        DECODE_PRINTF(",");
        srcreg = DECODE_RM_BYTE_REGISTER(rh);
        DECODE_PRINTF("\n");
        TRACE_AND_STEP();
        store_data_byte(destoffset, *srcreg);
    } else { /* register to register */
        destreg = DECODE_RM_BYTE_REGISTER(rl);
        DECODE_PRINTF(",");
        srcreg = DECODE_RM_BYTE_REGISTER(rh);
        DECODE_PRINTF("\n");
        TRACE_AND_STEP();
        *destreg = *srcreg;
    }
    DECODE_CLEAR_SEGOVR();
    END_OF_INSTR();
}

/****************************************************************************
REMARKS:
Handles opcode 0x89
****************************************************************************/
func x86emuOp_mov_word_RM_R(_ u8) {
    var mod, rl, rh int
    var destoffset uint

    START_OF_INSTR();
    DECODE_PRINTF("MOV\t");
    FETCH_DECODE_MODRM(mod, rh, rl);
    if (mod < 3) {
        destoffset = decode_rmXX_address(mod, rl);
        if (M.x86.mode & SYSMODE_PREFIX_DATA) {
            var srcreg *u32

            DECODE_PRINTF(",");
            srcreg = DECODE_RM_LONG_REGISTER(rh);
            DECODE_PRINTF("\n");
            TRACE_AND_STEP();
            store_data_long(destoffset, *srcreg);
        } else {
            var srcreg *u16

            DECODE_PRINTF(",");
            srcreg = DECODE_RM_WORD_REGISTER(rh);
            DECODE_PRINTF("\n");
            TRACE_AND_STEP();
            store_data_word(destoffset, *srcreg);
        }
    } else { /* register to register */
        if (M.x86.mode & SYSMODE_PREFIX_DATA) {
            u32 *destreg,*srcreg;

            destreg = DECODE_RM_LONG_REGISTER(rl);
            DECODE_PRINTF(",");
            srcreg = DECODE_RM_LONG_REGISTER(rh);
            DECODE_PRINTF("\n");
            TRACE_AND_STEP();
            *destreg = *srcreg;
        } else {
            u16 *destreg,*srcreg;

            destreg = DECODE_RM_WORD_REGISTER(rl);
            DECODE_PRINTF(",");
            srcreg = DECODE_RM_WORD_REGISTER(rh);
            DECODE_PRINTF("\n");
            TRACE_AND_STEP();
            *destreg = *srcreg;
        }
    }
    DECODE_CLEAR_SEGOVR();
    END_OF_INSTR();
}

/****************************************************************************
REMARKS:
Handles opcode 0x8a
****************************************************************************/
func x86emuOp_mov_byte_R_RM(_ u8) {
    var mod, rl, rh int
    var destreg, srcreg *u8
    var srcoffset uint
    var srcval u8

    START_OF_INSTR();
    DECODE_PRINTF("MOV\t");
    FETCH_DECODE_MODRM(mod, rh, rl);
    if (mod < 3) {
        destreg = DECODE_RM_BYTE_REGISTER(rh);
        DECODE_PRINTF(",");
        srcoffset = decode_rmXX_address(mod, rl);
        srcval = fetch_data_byte(srcoffset);
        DECODE_PRINTF("\n");
        TRACE_AND_STEP();
        *destreg = srcval;
    } else { /* register to register */
        destreg = DECODE_RM_BYTE_REGISTER(rh);
        DECODE_PRINTF(",");
        srcreg = DECODE_RM_BYTE_REGISTER(rl);
        DECODE_PRINTF("\n");
        TRACE_AND_STEP();
        *destreg = *srcreg;
    }
    DECODE_CLEAR_SEGOVR();
    END_OF_INSTR();
}

/****************************************************************************
REMARKS:
Handles opcode 0x8b
****************************************************************************/
func x86emuOp_mov_word_R_RM(_ u8) {
    var mod, rl, rh int
    var srcoffset uint

    START_OF_INSTR();
    DECODE_PRINTF("MOV\t");
    FETCH_DECODE_MODRM(mod, rh, rl);
    if (mod < 3) {
        if (M.x86.mode & SYSMODE_PREFIX_DATA) {
            u32 *destreg;
            var srcval u32

            destreg = DECODE_RM_LONG_REGISTER(rh);
            DECODE_PRINTF(",");
            srcoffset = decode_rmXX_address(mod, rl);
            srcval = fetch_data_long(srcoffset);
            DECODE_PRINTF("\n");
            TRACE_AND_STEP();
            *destreg = srcval;
        } else {
            var destreg *u16
            u16 srcval;

            destreg = DECODE_RM_WORD_REGISTER(rh);
            DECODE_PRINTF(",");
            srcoffset = decode_rmXX_address(mod, rl);
            srcval = fetch_data_word(srcoffset);
            DECODE_PRINTF("\n");
            TRACE_AND_STEP();
            *destreg = srcval;
        }
    } else { /* register to register */
        if (M.x86.mode & SYSMODE_PREFIX_DATA) {
            var destreg, srcreg *u32

            destreg = DECODE_RM_LONG_REGISTER(rh);
            DECODE_PRINTF(",");
            srcreg = DECODE_RM_LONG_REGISTER(rl);
            DECODE_PRINTF("\n");
            TRACE_AND_STEP();
            *destreg = *srcreg;
        } else {
            var destreg, srcreg *u16

            destreg = DECODE_RM_WORD_REGISTER(rh);
            DECODE_PRINTF(",");
            srcreg = DECODE_RM_WORD_REGISTER(rl);
            DECODE_PRINTF("\n");
            TRACE_AND_STEP();
            *destreg = *srcreg;
        }
    }
    DECODE_CLEAR_SEGOVR();
    END_OF_INSTR();
}

/****************************************************************************
REMARKS:
Handles opcode 0x8c
****************************************************************************/
func x86emuOp_mov_word_RM_SR(_ u8) {
    var mod, rl, rh int
    var destreg, srcreg *u16
    var destoffset uint
    var destval u16

    START_OF_INSTR();
    DECODE_PRINTF("MOV\t");
    FETCH_DECODE_MODRM(mod, rh, rl);
    if (mod < 3) {
        destoffset = decode_rmXX_address(mod, rl);
        DECODE_PRINTF(",");
        srcreg = decode_rm_seg_register(rh);
        DECODE_PRINTF("\n");
        TRACE_AND_STEP();
        destval = *srcreg;
        store_data_word(destoffset, destval);
    } else { /* register to register */
        destreg = DECODE_RM_WORD_REGISTER(rl);
        DECODE_PRINTF(",");
        srcreg = decode_rm_seg_register(rh);
        DECODE_PRINTF("\n");
        TRACE_AND_STEP();
        *destreg = *srcreg;
    }
    DECODE_CLEAR_SEGOVR();
    END_OF_INSTR();
}

/****************************************************************************
REMARKS:
Handles opcode 0x8d
****************************************************************************/
func x86emuOp_lea_word_R_M(_ u8) {
    var mod, rl, rh int
    var destoffset uint

    START_OF_INSTR();
    DECODE_PRINTF("LEA\t");
    FETCH_DECODE_MODRM(mod, rh, rl);
    if (mod < 3) {
        if (M.x86.mode & SYSMODE_PREFIX_ADDR) {
            u32 *srcreg = DECODE_RM_LONG_REGISTER(rh);
            DECODE_PRINTF(",");
            destoffset = decode_rmXX_address(mod, rl);
            DECODE_PRINTF("\n");
            TRACE_AND_STEP();
            *srcreg = (u32)destoffset;
 } else {
            u16 *srcreg = DECODE_RM_WORD_REGISTER(rh);
            DECODE_PRINTF(",");
            destoffset = decode_rmXX_address(mod, rl);
            DECODE_PRINTF("\n");
            TRACE_AND_STEP();
            *srcreg = (u16)destoffset;
        }
    }
    /* else { undefined.  Do nothing. } */
    DECODE_CLEAR_SEGOVR();
    END_OF_INSTR();
}

/****************************************************************************
REMARKS:
Handles opcode 0x8e
****************************************************************************/
func x86emuOp_mov_word_SR_RM(_ u8) {
    var mod, rl, rh int
    var destreg, srcreg *u16
    var srcoffset uint
    u16 srcval;

    START_OF_INSTR();
    DECODE_PRINTF("MOV\t");
    FETCH_DECODE_MODRM(mod, rh, rl);
    if (mod < 3) {
        destreg = decode_rm_seg_register(rh);
        DECODE_PRINTF(",");
        srcoffset = decode_rmXX_address(mod, rl);
        srcval = fetch_data_word(srcoffset);
        DECODE_PRINTF("\n");
        TRACE_AND_STEP();
        *destreg = srcval;
    } else { /* register to register */
        destreg = decode_rm_seg_register(rh);
        DECODE_PRINTF(",");
        srcreg = DECODE_RM_WORD_REGISTER(rl);
        DECODE_PRINTF("\n");
        TRACE_AND_STEP();
        *destreg = *srcreg;
    }
    /*
     * Clean up, and reset all the R_xSP pointers to the correct
     * locations.  This is about 3x too much overhead (doing all the
     * segreg ptrs when only one is needed, but this instruction
     * *cannot* be that common, and this isn't too much work anyway.
     */
    DECODE_CLEAR_SEGOVR();
    END_OF_INSTR();
}

/****************************************************************************
REMARKS:
Handles opcode 0x8f
****************************************************************************/
func x86emuOp_pop_RM(_ u8) {
    var mod, rl, rh int
    var destoffset uint

    START_OF_INSTR();
    DECODE_PRINTF("POP\t");
    FETCH_DECODE_MODRM(mod, rh, rl);
    if (rh != 0) {
        DECODE_PRINTF("ILLEGAL DECODE OF OPCODE 8F\n");
        HALT_SYS();
    }
    if (mod < 3) {
        destoffset = decode_rmXX_address(mod, rl);
        if (M.x86.mode & SYSMODE_PREFIX_DATA) {
            var destval u32

            DECODE_PRINTF("\n");
            TRACE_AND_STEP();
            destval = pop_long();
            store_data_long(destoffset, destval);
        } else {
            var destval u16

            DECODE_PRINTF("\n");
            TRACE_AND_STEP();
            destval = pop_word();
            store_data_word(destoffset, destval);
        }
    } else { /* register to register */
        if (M.x86.mode & SYSMODE_PREFIX_DATA) {
            u32 *destreg;

            destreg = DECODE_RM_LONG_REGISTER(rl);
            DECODE_PRINTF("\n");
            TRACE_AND_STEP();
            *destreg = pop_long();
        } else {
            var destreg *u16

            destreg = DECODE_RM_WORD_REGISTER(rl);
            DECODE_PRINTF("\n");
            TRACE_AND_STEP();
            *destreg = pop_word();
        }
    }
    DECODE_CLEAR_SEGOVR();
    END_OF_INSTR();
}

/****************************************************************************
REMARKS:
Handles opcode 0x90
****************************************************************************/
func x86emuOp_nop(_ u8) {
    START_OF_INSTR();
    DECODE_PRINTF("NOP\n");
    TRACE_AND_STEP();
    DECODE_CLEAR_SEGOVR();
    END_OF_INSTR();
}

/****************************************************************************
REMARKS:
Handles opcode 0x91-0x97
****************************************************************************/
func x86emuOp_xchg_word_AX_register(_ u8) {
    u32 tmp;

    op1 &= 0x7;

    START_OF_INSTR();

    if (M.x86.mode & SYSMODE_PREFIX_DATA) {
        u32 *reg32;
        DECODE_PRINTF("XCHG\tEAX,");
        reg32 = DECODE_RM_LONG_REGISTER(op1);
        DECODE_PRINTF("\n");
        TRACE_AND_STEP();
        tmp = M.x86.R_EAX;
        M.x86.R_EAX = *reg32;
        *reg32 = tmp;
    } else {
        u16 *reg16;
        DECODE_PRINTF("XCHG\tAX,");
        reg16 = DECODE_RM_WORD_REGISTER(op1);
        DECODE_PRINTF("\n");
        TRACE_AND_STEP();
        tmp = M.x86.R_AX;
        M.x86.R_AX = *reg16;
        *reg16 = (u16)tmp;
    }
    DECODE_CLEAR_SEGOVR();
    END_OF_INSTR();
}

/****************************************************************************
REMARKS:
Handles opcode 0x98
****************************************************************************/
func x86emuOp_cbw(_ u8) {
    START_OF_INSTR();
    if (M.x86.mode & SYSMODE_PREFIX_DATA) {
        DECODE_PRINTF("CWDE\n");
    } else {
        DECODE_PRINTF("CBW\n");
    }
    TRACE_AND_STEP();
    if (M.x86.mode & SYSMODE_PREFIX_DATA) {
        if (M.x86.R_AX & 0x8000) {
            M.x86.R_EAX |= 0xffff0000;
        } else {
            M.x86.R_EAX &= 0x0000ffff;
        }
    } else {
        if (M.x86.R_AL & 0x80) {
            M.x86.R_AH = 0xff;
        } else {
            M.x86.R_AH = 0x0;
        }
    }
    DECODE_CLEAR_SEGOVR();
    END_OF_INSTR();
}

/****************************************************************************
REMARKS:
Handles opcode 0x99
****************************************************************************/
func x86emuOp_cwd(_ u8) {
    START_OF_INSTR();
    if (M.x86.mode & SYSMODE_PREFIX_DATA) {
        DECODE_PRINTF("CDQ\n");
    } else {
        DECODE_PRINTF("CWD\n");
    }
    DECODE_PRINTF("CWD\n");
    TRACE_AND_STEP();
    if (M.x86.mode & SYSMODE_PREFIX_DATA) {
        if (M.x86.R_EAX & 0x80000000) {
            M.x86.R_EDX = 0xffffffff;
        } else {
            M.x86.R_EDX = 0x0;
        }
    } else {
        if (M.x86.R_AX & 0x8000) {
            M.x86.R_DX = 0xffff;
        } else {
            M.x86.R_DX = 0x0;
        }
    }
    DECODE_CLEAR_SEGOVR();
    END_OF_INSTR();
}

/****************************************************************************
REMARKS:
Handles opcode 0x9a
****************************************************************************/
func x86emuOp_call_far_IMM(_ u8) {
    u32 farseg, faroff;

    START_OF_INSTR();
    DECODE_PRINTF("CALL\t");
    if (M.x86.mode & SYSMODE_PREFIX_DATA) {
        faroff = fetch_long_imm();
        farseg = fetch_word_imm();
    } else {
        faroff = fetch_word_imm();
        farseg = fetch_word_imm();
    }
    DECODE_PRINTF2("%04x:", farseg);
    DECODE_PRINTF2("%04x\n", faroff);
    CALL_TRACE(M.x86.saved_cs, M.x86.saved_ip, farseg, faroff, "FAR ");

    /* XXX
     *
     * Hooked interrupt vectors calling into our "BIOS" will cause
     * problems unless all intersegment stuff is checked for BIOS
     * access.  Check needed here.  For moment, let it alone.
     */
    TRACE_AND_STEP();
    push_word(M.x86.R_CS);
    M.x86.R_CS = farseg;
    if (M.x86.mode & SYSMODE_PREFIX_DATA) {
        push_long(M.x86.R_EIP);
    } else {
        push_word(M.x86.R_IP);
    }
    M.x86.R_EIP = faroff & 0xffff;
    DECODE_CLEAR_SEGOVR();
    END_OF_INSTR();
}

/****************************************************************************
REMARKS:
Handles opcode 0x9b
****************************************************************************/
func x86emuOp_wait(_ u8) {
    START_OF_INSTR();
    DECODE_PRINTF("WAIT");
    TRACE_AND_STEP();
    /* NADA.  */
    DECODE_CLEAR_SEGOVR();
    END_OF_INSTR();
}

/****************************************************************************
REMARKS:
Handles opcode 0x9c
****************************************************************************/
func x86emuOp_pushf_word(_ u8) {
    u32 flags;

    START_OF_INSTR();
    if (M.x86.mode & SYSMODE_PREFIX_DATA) {
        DECODE_PRINTF("PUSHFD\n");
    } else {
        DECODE_PRINTF("PUSHF\n");
    }
    TRACE_AND_STEP();

    /* clear out *all* bits not representing flags, and turn on real bits */
    flags = (M.x86.R_EFLG & F_MSK) | F_ALWAYS_ON;
    if (M.x86.mode & SYSMODE_PREFIX_DATA) {
        push_long(flags);
    } else {
        push_word((u16)flags);
    }
    DECODE_CLEAR_SEGOVR();
    END_OF_INSTR();
}

/****************************************************************************
REMARKS:
Handles opcode 0x9d
****************************************************************************/
func x86emuOp_popf_word(_ u8) {
    START_OF_INSTR();
    if (M.x86.mode & SYSMODE_PREFIX_DATA) {
        DECODE_PRINTF("POPFD\n");
    } else {
        DECODE_PRINTF("POPF\n");
    }
    TRACE_AND_STEP();
    if (M.x86.mode & SYSMODE_PREFIX_DATA) {
        M.x86.R_EFLG = pop_long();
    } else {
        M.x86.R_FLG = pop_word();
    }
    DECODE_CLEAR_SEGOVR();
    END_OF_INSTR();
}

/****************************************************************************
REMARKS:
Handles opcode 0x9e
****************************************************************************/
func x86emuOp_sahf(_ u8) {
    START_OF_INSTR();
    DECODE_PRINTF("SAHF\n");
    TRACE_AND_STEP();
    /* clear the lower bits of the flag register */
    M.x86.R_FLG &= 0xffffff00;
    /* or in the AH register into the flags register */
    M.x86.R_FLG |= M.x86.R_AH;
    DECODE_CLEAR_SEGOVR();
    END_OF_INSTR();
}

/****************************************************************************
REMARKS:
Handles opcode 0x9f
****************************************************************************/
func x86emuOp_lahf(_ u8) {
    START_OF_INSTR();
    DECODE_PRINTF("LAHF\n");
    TRACE_AND_STEP();
 M.x86.R_AH = (u8)(M.x86.R_FLG & 0xff);
    /*undocumented TC++ behavior??? Nope.  It's documented, but
       you have too look real hard to notice it. */
    M.x86.R_AH |= 0x2;
    DECODE_CLEAR_SEGOVR();
    END_OF_INSTR();
}

/****************************************************************************
REMARKS:
Handles opcode 0xa0
****************************************************************************/
func x86emuOp_mov_AL_M_IMM(_ u8) {
    u16 offset;

    START_OF_INSTR();
    DECODE_PRINTF("MOV\tAL,");
    offset = fetch_word_imm();
    DECODE_PRINTF2("[%04x]\n", offset);
    TRACE_AND_STEP();
    M.x86.R_AL = fetch_data_byte(offset);
    DECODE_CLEAR_SEGOVR();
    END_OF_INSTR();
}

/****************************************************************************
REMARKS:
Handles opcode 0xa1
****************************************************************************/
func x86emuOp_mov_AX_M_IMM(_ u8) {
    u16 offset;

    START_OF_INSTR();
    offset = fetch_word_imm();
    if (M.x86.mode & SYSMODE_PREFIX_DATA) {
        DECODE_PRINTF2("MOV\tEAX,[%04x]\n", offset);
    } else {
        DECODE_PRINTF2("MOV\tAX,[%04x]\n", offset);
    }
    TRACE_AND_STEP();
    if (M.x86.mode & SYSMODE_PREFIX_DATA) {
        M.x86.R_EAX = fetch_data_long(offset);
    } else {
        M.x86.R_AX = fetch_data_word(offset);
    }
    DECODE_CLEAR_SEGOVR();
    END_OF_INSTR();
}

/****************************************************************************
REMARKS:
Handles opcode 0xa2
****************************************************************************/
func x86emuOp_mov_M_AL_IMM(_ u8) {
    u16 offset;

    START_OF_INSTR();
    DECODE_PRINTF("MOV\t");
    offset = fetch_word_imm();
    DECODE_PRINTF2("[%04x],AL\n", offset);
    TRACE_AND_STEP();
    store_data_byte(offset, M.x86.R_AL);
    DECODE_CLEAR_SEGOVR();
    END_OF_INSTR();
}

/****************************************************************************
REMARKS:
Handles opcode 0xa3
****************************************************************************/
func x86emuOp_mov_M_AX_IMM(_ u8) {
    u16 offset;

    START_OF_INSTR();
    offset = fetch_word_imm();
    if (M.x86.mode & SYSMODE_PREFIX_DATA) {
        DECODE_PRINTF2("MOV\t[%04x],EAX\n", offset);
    } else {
        DECODE_PRINTF2("MOV\t[%04x],AX\n", offset);
    }
    TRACE_AND_STEP();
    if (M.x86.mode & SYSMODE_PREFIX_DATA) {
        store_data_long(offset, M.x86.R_EAX);
    } else {
        store_data_word(offset, M.x86.R_AX);
    }
    DECODE_CLEAR_SEGOVR();
    END_OF_INSTR();
}

/****************************************************************************
REMARKS:
Handles opcode 0xa4
****************************************************************************/
func x86emuOp_movs_byte(_ u8) {
    u8 val;
    u32 count;
    int inc;

    START_OF_INSTR();
    DECODE_PRINTF("MOVS\tBYTE\n");
    if (ACCESS_FLAG(F_DF)) /* down */
        inc = -1;
    else
        inc = 1;
    TRACE_AND_STEP();
    count = 1;
    if (M.x86.mode & (SYSMODE_PREFIX_REPE | SYSMODE_PREFIX_REPNE)) {
        /* don't care whether REPE or REPNE */
        /* move them until (E)CX is ZERO. */
        count = (M.x86.mode & SYSMODE_32BIT_REP) ? M.x86.R_ECX : M.x86.R_CX;
        M.x86.R_CX = 0;
 if (M.x86.mode & SYSMODE_32BIT_REP)
            M.x86.R_ECX = 0;
        M.x86.mode &= ~(SYSMODE_PREFIX_REPE | SYSMODE_PREFIX_REPNE);
    }
    while (count--) {
        val = fetch_data_byte(M.x86.R_SI);
        store_data_byte_abs(M.x86.R_ES, M.x86.R_DI, val);
        M.x86.R_SI += inc;
        M.x86.R_DI += inc;
        if (M.x86.intr & INTR_HALTED)
            break;
    }
    DECODE_CLEAR_SEGOVR();
    END_OF_INSTR();
}

/****************************************************************************
REMARKS:
Handles opcode 0xa5
****************************************************************************/
func x86emuOp_movs_word(_ u8) {
    u32 val;
    int inc;
    u32 count;

    START_OF_INSTR();
    if (M.x86.mode & SYSMODE_PREFIX_DATA) {
        DECODE_PRINTF("MOVS\tDWORD\n");
        if (ACCESS_FLAG(F_DF)) /* down */
            inc = -4;
        else
            inc = 4;
    } else {
        DECODE_PRINTF("MOVS\tWORD\n");
        if (ACCESS_FLAG(F_DF)) /* down */
            inc = -2;
        else
            inc = 2;
    }
    TRACE_AND_STEP();
    count = 1;
    if (M.x86.mode & (SYSMODE_PREFIX_REPE | SYSMODE_PREFIX_REPNE)) {
        /* don't care whether REPE or REPNE */
        /* move them until (E)CX is ZERO. */
        count = (M.x86.mode & SYSMODE_32BIT_REP) ? M.x86.R_ECX : M.x86.R_CX;
        M.x86.R_CX = 0;
 if (M.x86.mode & SYSMODE_32BIT_REP)
            M.x86.R_ECX = 0;
        M.x86.mode &= ~(SYSMODE_PREFIX_REPE | SYSMODE_PREFIX_REPNE);
    }
    while (count--) {
        if (M.x86.mode & SYSMODE_PREFIX_DATA) {
            val = fetch_data_long(M.x86.R_SI);
            store_data_long_abs(M.x86.R_ES, M.x86.R_DI, val);
        } else {
            val = fetch_data_word(M.x86.R_SI);
            store_data_word_abs(M.x86.R_ES, M.x86.R_DI, (u16)val);
        }
        M.x86.R_SI += inc;
        M.x86.R_DI += inc;
        if (M.x86.intr & INTR_HALTED)
            break;
    }
    DECODE_CLEAR_SEGOVR();
    END_OF_INSTR();
}

/****************************************************************************
REMARKS:
Handles opcode 0xa6
****************************************************************************/
func x86emuOp_cmps_byte(_ u8) {
    s8 val1, val2;
    int inc;

    START_OF_INSTR();
    DECODE_PRINTF("CMPS\tBYTE\n");
    TRACE_AND_STEP();
    if (ACCESS_FLAG(F_DF)) /* down */
        inc = -1;
    else
        inc = 1;

    if (M.x86.mode & (SYSMODE_PREFIX_REPE | SYSMODE_PREFIX_REPNE)) {
        /* REPE  */
        /* move them until (E)CX is ZERO. */
        while (((M.x86.mode & SYSMODE_32BIT_REP) ? M.x86.R_ECX : M.x86.R_CX) != 0) {
            val1 = fetch_data_byte(M.x86.R_SI);
            val2 = fetch_data_byte_abs(M.x86.R_ES, M.x86.R_DI);
                     cmp_byte(val1, val2);
            if (M.x86.mode & SYSMODE_32BIT_REP)
                M.x86.R_ECX -= 1;
            else
                M.x86.R_CX -= 1;
            M.x86.R_SI += inc;
            M.x86.R_DI += inc;
            if ( (M.x86.mode & SYSMODE_PREFIX_REPE) && (ACCESS_FLAG(F_ZF) == 0) ) break;
            if ( (M.x86.mode & SYSMODE_PREFIX_REPNE) && ACCESS_FLAG(F_ZF) ) break;
            if (M.x86.intr & INTR_HALTED)
                break;
        }
        M.x86.mode &= ~(SYSMODE_PREFIX_REPE | SYSMODE_PREFIX_REPNE);
    } else {
        val1 = fetch_data_byte(M.x86.R_SI);
        val2 = fetch_data_byte_abs(M.x86.R_ES, M.x86.R_DI);
        cmp_byte(val1, val2);
        M.x86.R_SI += inc;
        M.x86.R_DI += inc;
    }
    DECODE_CLEAR_SEGOVR();
    END_OF_INSTR();
}

/****************************************************************************
REMARKS:
Handles opcode 0xa7
****************************************************************************/
func x86emuOp_cmps_word(_ u8) {
    u32 val1,val2;
    int inc;

    START_OF_INSTR();
    if (M.x86.mode & SYSMODE_PREFIX_DATA) {
        DECODE_PRINTF("CMPS\tDWORD\n");
        inc = 4;
    } else {
        DECODE_PRINTF("CMPS\tWORD\n");
        inc = 2;
    }
    if (ACCESS_FLAG(F_DF)) /* down */
        inc = -inc;

    TRACE_AND_STEP();
    if (M.x86.mode & (SYSMODE_PREFIX_REPE | SYSMODE_PREFIX_REPNE)) {
        /* REPE  */
        /* move them until (E)CX is ZERO. */
        while (((M.x86.mode & SYSMODE_32BIT_REP) ? M.x86.R_ECX : M.x86.R_CX) != 0) {
            if (M.x86.mode & SYSMODE_PREFIX_DATA) {
                val1 = fetch_data_long(M.x86.R_SI);
                val2 = fetch_data_long_abs(M.x86.R_ES, M.x86.R_DI);
                cmp_long(val1, val2);
            } else {
                val1 = fetch_data_word(M.x86.R_SI);
                val2 = fetch_data_word_abs(M.x86.R_ES, M.x86.R_DI);
                cmp_word((u16)val1, (u16)val2);
            }
            if (M.x86.mode & SYSMODE_32BIT_REP)
                M.x86.R_ECX -= 1;
            else
                M.x86.R_CX -= 1;
            M.x86.R_SI += inc;
            M.x86.R_DI += inc;
            if ( (M.x86.mode & SYSMODE_PREFIX_REPE) && ACCESS_FLAG(F_ZF) == 0 ) break;
            if ( (M.x86.mode & SYSMODE_PREFIX_REPNE) && ACCESS_FLAG(F_ZF) ) break;
            if (M.x86.intr & INTR_HALTED)
                break;
        }
        M.x86.mode &= ~(SYSMODE_PREFIX_REPE | SYSMODE_PREFIX_REPNE);
    } else {
        if (M.x86.mode & SYSMODE_PREFIX_DATA) {
            val1 = fetch_data_long(M.x86.R_SI);
            val2 = fetch_data_long_abs(M.x86.R_ES, M.x86.R_DI);
            cmp_long(val1, val2);
        } else {
            val1 = fetch_data_word(M.x86.R_SI);
            val2 = fetch_data_word_abs(M.x86.R_ES, M.x86.R_DI);
            cmp_word((u16)val1, (u16)val2);
        }
        M.x86.R_SI += inc;
        M.x86.R_DI += inc;
    }
    DECODE_CLEAR_SEGOVR();
    END_OF_INSTR();
}

/****************************************************************************
REMARKS:
Handles opcode 0xa8
****************************************************************************/
func x86emuOp_test_AL_IMM(_ u8) {
    int imm;

    START_OF_INSTR();
    DECODE_PRINTF("TEST\tAL,");
    imm = fetch_byte_imm();
    DECODE_PRINTF2("%04x\n", imm);
    TRACE_AND_STEP();
 test_byte(M.x86.R_AL, (u8)imm);
    DECODE_CLEAR_SEGOVR();
    END_OF_INSTR();
}

/****************************************************************************
REMARKS:
Handles opcode 0xa9
****************************************************************************/
func x86emuOp_test_AX_IMM(_ u8) {
    var srcval u32

    START_OF_INSTR();
    if (M.x86.mode & SYSMODE_PREFIX_DATA) {
        DECODE_PRINTF("TEST\tEAX,");
        srcval = fetch_long_imm();
    } else {
        DECODE_PRINTF("TEST\tAX,");
        srcval = fetch_word_imm();
    }
    DECODE_PRINTF2("%x\n", srcval);
    TRACE_AND_STEP();
    if (M.x86.mode & SYSMODE_PREFIX_DATA) {
        test_long(M.x86.R_EAX, srcval);
    } else {
        test_word(M.x86.R_AX, (u16)srcval);
    }
    DECODE_CLEAR_SEGOVR();
    END_OF_INSTR();
}

/****************************************************************************
REMARKS:
Handles opcode 0xaa
****************************************************************************/
func x86emuOp_stos_byte(_ u8) {
    int inc;

    START_OF_INSTR();
    DECODE_PRINTF("STOS\tBYTE\n");
    if (ACCESS_FLAG(F_DF)) /* down */
        inc = -1;
    else
        inc = 1;
    TRACE_AND_STEP();
    if (M.x86.mode & (SYSMODE_PREFIX_REPE | SYSMODE_PREFIX_REPNE)) {
        /* don't care whether REPE or REPNE */
        /* move them until (E)CX is ZERO. */
        while (((M.x86.mode & SYSMODE_32BIT_REP) ? M.x86.R_ECX : M.x86.R_CX) != 0) {
            store_data_byte_abs(M.x86.R_ES, M.x86.R_DI, M.x86.R_AL);
            if (M.x86.mode & SYSMODE_32BIT_REP)
                M.x86.R_ECX -= 1;
            else
                M.x86.R_CX -= 1;
            M.x86.R_DI += inc;
            if (M.x86.intr & INTR_HALTED)
                break;
        }
        M.x86.mode &= ~(SYSMODE_PREFIX_REPE | SYSMODE_PREFIX_REPNE);
    } else {
        store_data_byte_abs(M.x86.R_ES, M.x86.R_DI, M.x86.R_AL);
        M.x86.R_DI += inc;
    }
    DECODE_CLEAR_SEGOVR();
    END_OF_INSTR();
}

/****************************************************************************
REMARKS:
Handles opcode 0xab
****************************************************************************/
func x86emuOp_stos_word(_ u8) {
    int inc;
    u32 count;

    START_OF_INSTR();
    if (M.x86.mode & SYSMODE_PREFIX_DATA) {
        DECODE_PRINTF("STOS\tDWORD\n");
        if (ACCESS_FLAG(F_DF)) /* down */
            inc = -4;
        else
            inc = 4;
    } else {
        DECODE_PRINTF("STOS\tWORD\n");
        if (ACCESS_FLAG(F_DF)) /* down */
            inc = -2;
        else
            inc = 2;
    }
    TRACE_AND_STEP();
    count = 1;
    if (M.x86.mode & (SYSMODE_PREFIX_REPE | SYSMODE_PREFIX_REPNE)) {
        /* don't care whether REPE or REPNE */
        /* move them until (E)CX is ZERO. */
        count = (M.x86.mode & SYSMODE_32BIT_REP) ? M.x86.R_ECX : M.x86.R_CX;
        M.x86.R_CX = 0;
 if (M.x86.mode & SYSMODE_32BIT_REP)
            M.x86.R_ECX = 0;
        M.x86.mode &= ~(SYSMODE_PREFIX_REPE | SYSMODE_PREFIX_REPNE);
    }
    while (count--) {
        if (M.x86.mode & SYSMODE_PREFIX_DATA) {
            store_data_long_abs(M.x86.R_ES, M.x86.R_DI, M.x86.R_EAX);
        } else {
            store_data_word_abs(M.x86.R_ES, M.x86.R_DI, M.x86.R_AX);
        }
        M.x86.R_DI += inc;
        if (M.x86.intr & INTR_HALTED)
            break;
    }
    DECODE_CLEAR_SEGOVR();
    END_OF_INSTR();
}

/****************************************************************************
REMARKS:
Handles opcode 0xac
****************************************************************************/
func x86emuOp_lods_byte(_ u8) {
    int inc;

    START_OF_INSTR();
    DECODE_PRINTF("LODS\tBYTE\n");
    TRACE_AND_STEP();
    if (ACCESS_FLAG(F_DF)) /* down */
        inc = -1;
    else
        inc = 1;
    if (M.x86.mode & (SYSMODE_PREFIX_REPE | SYSMODE_PREFIX_REPNE)) {
        /* don't care whether REPE or REPNE */
        /* move them until (E)CX is ZERO. */
        while (((M.x86.mode & SYSMODE_32BIT_REP) ? M.x86.R_ECX : M.x86.R_CX) != 0) {
            M.x86.R_AL = fetch_data_byte(M.x86.R_SI);
            if (M.x86.mode & SYSMODE_32BIT_REP)
                M.x86.R_ECX -= 1;
            else
                M.x86.R_CX -= 1;
            M.x86.R_SI += inc;
            if (M.x86.intr & INTR_HALTED)
                break;
        }
        M.x86.mode &= ~(SYSMODE_PREFIX_REPE | SYSMODE_PREFIX_REPNE);
    } else {
        M.x86.R_AL = fetch_data_byte(M.x86.R_SI);
        M.x86.R_SI += inc;
    }
    DECODE_CLEAR_SEGOVR();
    END_OF_INSTR();
}

/****************************************************************************
REMARKS:
Handles opcode 0xad
****************************************************************************/
func x86emuOp_lods_word(_ u8) {
    int inc;
    u32 count;

    START_OF_INSTR();
    if (M.x86.mode & SYSMODE_PREFIX_DATA) {
        DECODE_PRINTF("LODS\tDWORD\n");
        if (ACCESS_FLAG(F_DF)) /* down */
            inc = -4;
        else
            inc = 4;
    } else {
        DECODE_PRINTF("LODS\tWORD\n");
        if (ACCESS_FLAG(F_DF)) /* down */
            inc = -2;
        else
            inc = 2;
    }
    TRACE_AND_STEP();
    count = 1;
    if (M.x86.mode & (SYSMODE_PREFIX_REPE | SYSMODE_PREFIX_REPNE)) {
        /* don't care whether REPE or REPNE */
        /* move them until (E)CX is ZERO. */
        count = (M.x86.mode & SYSMODE_32BIT_REP) ? M.x86.R_ECX : M.x86.R_CX;
        M.x86.R_CX = 0;
 if (M.x86.mode & SYSMODE_32BIT_REP)
            M.x86.R_ECX = 0;
        M.x86.mode &= ~(SYSMODE_PREFIX_REPE | SYSMODE_PREFIX_REPNE);
    }
    while (count--) {
        if (M.x86.mode & SYSMODE_PREFIX_DATA) {
            M.x86.R_EAX = fetch_data_long(M.x86.R_SI);
        } else {
            M.x86.R_AX = fetch_data_word(M.x86.R_SI);
        }
        M.x86.R_SI += inc;
        if (M.x86.intr & INTR_HALTED)
            break;
    }
    DECODE_CLEAR_SEGOVR();
    END_OF_INSTR();
}

/****************************************************************************
REMARKS:
Handles opcode 0xae
****************************************************************************/
func x86emuOp_scas_byte(_ u8) {
    s8 val2;
    int inc;

    START_OF_INSTR();
    DECODE_PRINTF("SCAS\tBYTE\n");
    TRACE_AND_STEP();
    if (ACCESS_FLAG(F_DF)) /* down */
        inc = -1;
    else
        inc = 1;
    if (M.x86.mode & SYSMODE_PREFIX_REPE) {
        /* REPE  */
        /* move them until (E)CX is ZERO. */
        while (((M.x86.mode & SYSMODE_32BIT_REP) ? M.x86.R_ECX : M.x86.R_CX) != 0) {
            val2 = fetch_data_byte_abs(M.x86.R_ES, M.x86.R_DI);
            cmp_byte(M.x86.R_AL, val2);
            if (M.x86.mode & SYSMODE_32BIT_REP)
                M.x86.R_ECX -= 1;
            else
                M.x86.R_CX -= 1;
            M.x86.R_DI += inc;
            if (ACCESS_FLAG(F_ZF) == 0)
                break;
            if (M.x86.intr & INTR_HALTED)
                break;
        }
        M.x86.mode &= ~SYSMODE_PREFIX_REPE;
    } else if (M.x86.mode & SYSMODE_PREFIX_REPNE) {
        /* REPNE  */
        /* move them until (E)CX is ZERO. */
        while (((M.x86.mode & SYSMODE_32BIT_REP) ? M.x86.R_ECX : M.x86.R_CX) != 0) {
            val2 = fetch_data_byte_abs(M.x86.R_ES, M.x86.R_DI);
            cmp_byte(M.x86.R_AL, val2);
            if (M.x86.mode & SYSMODE_32BIT_REP)
                M.x86.R_ECX -= 1;
            else
                M.x86.R_CX -= 1;
            M.x86.R_DI += inc;
            if (ACCESS_FLAG(F_ZF))
                break; /* zero flag set means equal */
            if (M.x86.intr & INTR_HALTED)
                break;
        }
        M.x86.mode &= ~SYSMODE_PREFIX_REPNE;
    } else {
        val2 = fetch_data_byte_abs(M.x86.R_ES, M.x86.R_DI);
        cmp_byte(M.x86.R_AL, val2);
        M.x86.R_DI += inc;
    }
    DECODE_CLEAR_SEGOVR();
    END_OF_INSTR();
}

/****************************************************************************
REMARKS:
Handles opcode 0xaf
****************************************************************************/
func x86emuOp_scas_word(_ u8) {
    int inc;
    u32 val;

    START_OF_INSTR();
    if (M.x86.mode & SYSMODE_PREFIX_DATA) {
        DECODE_PRINTF("SCAS\tDWORD\n");
        if (ACCESS_FLAG(F_DF)) /* down */
            inc = -4;
        else
            inc = 4;
    } else {
        DECODE_PRINTF("SCAS\tWORD\n");
        if (ACCESS_FLAG(F_DF)) /* down */
            inc = -2;
        else
            inc = 2;
    }
    TRACE_AND_STEP();
    if (M.x86.mode & SYSMODE_PREFIX_REPE) {
        /* REPE  */
        /* move them until (E)CX is ZERO. */
        while (((M.x86.mode & SYSMODE_32BIT_REP) ? M.x86.R_ECX : M.x86.R_CX) != 0) {
            if (M.x86.mode & SYSMODE_PREFIX_DATA) {
                val = fetch_data_long_abs(M.x86.R_ES, M.x86.R_DI);
                cmp_long(M.x86.R_EAX, val);
            } else {
                val = fetch_data_word_abs(M.x86.R_ES, M.x86.R_DI);
                cmp_word(M.x86.R_AX, (u16)val);
            }
            if (M.x86.mode & SYSMODE_32BIT_REP)
                M.x86.R_ECX -= 1;
            else
                M.x86.R_CX -= 1;
            M.x86.R_DI += inc;
            if (ACCESS_FLAG(F_ZF) == 0)
                break;
            if (M.x86.intr & INTR_HALTED)
                break;
        }
        M.x86.mode &= ~SYSMODE_PREFIX_REPE;
    } else if (M.x86.mode & SYSMODE_PREFIX_REPNE) {
        /* REPNE  */
        /* move them until (E)CX is ZERO. */
        while (((M.x86.mode & SYSMODE_32BIT_REP) ? M.x86.R_ECX : M.x86.R_CX) != 0) {
            if (M.x86.mode & SYSMODE_PREFIX_DATA) {
                val = fetch_data_long_abs(M.x86.R_ES, M.x86.R_DI);
                cmp_long(M.x86.R_EAX, val);
            } else {
                val = fetch_data_word_abs(M.x86.R_ES, M.x86.R_DI);
                cmp_word(M.x86.R_AX, (u16)val);
            }
            if (M.x86.mode & SYSMODE_32BIT_REP)
                M.x86.R_ECX -= 1;
            else
                M.x86.R_CX -= 1;
            M.x86.R_DI += inc;
            if (ACCESS_FLAG(F_ZF))
                break; /* zero flag set means equal */
            if (M.x86.intr & INTR_HALTED)
                break;
        }
        M.x86.mode &= ~SYSMODE_PREFIX_REPNE;
    } else {
        if (M.x86.mode & SYSMODE_PREFIX_DATA) {
            val = fetch_data_long_abs(M.x86.R_ES, M.x86.R_DI);
            cmp_long(M.x86.R_EAX, val);
        } else {
            val = fetch_data_word_abs(M.x86.R_ES, M.x86.R_DI);
            cmp_word(M.x86.R_AX, (u16)val);
        }
        M.x86.R_DI += inc;
    }
    DECODE_CLEAR_SEGOVR();
    END_OF_INSTR();
}

/****************************************************************************
REMARKS:
Handles opcode 0xb0 - 0xb7
****************************************************************************/
func x86emuOp_mov_byte_register_IMM(u8 op1) {
    u8 imm, *ptr;

    START_OF_INSTR();
    DECODE_PRINTF("MOV\t");
    ptr = DECODE_RM_BYTE_REGISTER(op1 & 0x7);
    DECODE_PRINTF(",");
    imm = fetch_byte_imm();
    DECODE_PRINTF2("%x\n", imm);
    TRACE_AND_STEP();
    *ptr = imm;
    DECODE_CLEAR_SEGOVR();
    END_OF_INSTR();
}

/****************************************************************************
REMARKS:
Handles opcode 0xb8 - 0xbf
****************************************************************************/
func x86emuOp_mov_word_register_IMM(_ u8) {
    var srcval u32

    op1 &= 0x7;

    START_OF_INSTR();
    DECODE_PRINTF("MOV\t");
    if (M.x86.mode & SYSMODE_PREFIX_DATA) {
        u32 *reg32;
        reg32 = DECODE_RM_LONG_REGISTER(op1);
        srcval = fetch_long_imm();
        DECODE_PRINTF2(",%x\n", srcval);
        TRACE_AND_STEP();
        *reg32 = srcval;
    } else {
        u16 *reg16;
        reg16 = DECODE_RM_WORD_REGISTER(op1);
        srcval = fetch_word_imm();
        DECODE_PRINTF2(",%x\n", srcval);
        TRACE_AND_STEP();
        *reg16 = (u16)srcval;
    }
    DECODE_CLEAR_SEGOVR();
    END_OF_INSTR();
}

/****************************************************************************
REMARKS:
Handles opcode 0xc0
****************************************************************************/
func x86emuOp_opcC0_byte_RM_MEM(_ u8) {
    var mod, rl, rh int
    u8 *destreg;
    var destoffset uint
    var destval u8
    u8 amt;

    /*
     * Yet another weirdo special case instruction format.  Part of
     * the opcode held below in "RH".  Doubly nested case would
     * result, except that the decoded instruction
     */
    START_OF_INSTR();
    FETCH_DECODE_MODRM(mod, rh, rl);

    if (DEBUG_DECODE()) {
        /* XXX DECODE_PRINTF may be changed to something more
           general, so that it is important to leave the strings
           in the same format, even though the result is that the
           above test is done twice. */

        switch (rh) {
        case 0:
            DECODE_PRINTF("ROL\t");
            break;
        case 1:
            DECODE_PRINTF("ROR\t");
            break;
        case 2:
            DECODE_PRINTF("RCL\t");
            break;
        case 3:
            DECODE_PRINTF("RCR\t");
            break;
        case 4:
            DECODE_PRINTF("SHL\t");
            break;
        case 5:
            DECODE_PRINTF("SHR\t");
            break;
        case 6:
            DECODE_PRINTF("SAL\t");
            break;
        case 7:
            DECODE_PRINTF("SAR\t");
            break;
        }
    }

    /* know operation, decode the mod byte to find the addressing
       mode. */
    if (mod < 3) {
        DECODE_PRINTF("BYTE PTR ");
        destoffset = decode_rmXX_address(mod, rl);
        amt = fetch_byte_imm();
        DECODE_PRINTF2(",%x\n", amt);
        destval = fetch_data_byte(destoffset);
        TRACE_AND_STEP();
        destval = (*opcD0_byte_operation[rh]) (destval, amt);
        store_data_byte(destoffset, destval);
    } else { /* register to register */
        destreg = DECODE_RM_BYTE_REGISTER(rl);
        amt = fetch_byte_imm();
        DECODE_PRINTF2(",%x\n", amt);
        TRACE_AND_STEP();
        destval = (*opcD0_byte_operation[rh]) (*destreg, amt);
        *destreg = destval;
    }
    DECODE_CLEAR_SEGOVR();
    END_OF_INSTR();
}

/****************************************************************************
REMARKS:
Handles opcode 0xc1
****************************************************************************/
func x86emuOp_opcC1_word_RM_MEM(_ u8) {
    var mod, rl, rh int
    var destoffset uint
    u8 amt;

    /*
     * Yet another weirdo special case instruction format.  Part of
     * the opcode held below in "RH".  Doubly nested case would
     * result, except that the decoded instruction
     */
    START_OF_INSTR();
    FETCH_DECODE_MODRM(mod, rh, rl);

    if (DEBUG_DECODE()) {
        /* XXX DECODE_PRINTF may be changed to something more
           general, so that it is important to leave the strings
           in the same format, even though the result is that the
           above test is done twice. */

        switch (rh) {
        case 0:
            DECODE_PRINTF("ROL\t");
            break;
        case 1:
            DECODE_PRINTF("ROR\t");
            break;
        case 2:
            DECODE_PRINTF("RCL\t");
            break;
        case 3:
            DECODE_PRINTF("RCR\t");
            break;
        case 4:
            DECODE_PRINTF("SHL\t");
            break;
        case 5:
            DECODE_PRINTF("SHR\t");
            break;
        case 6:
            DECODE_PRINTF("SAL\t");
            break;
        case 7:
            DECODE_PRINTF("SAR\t");
            break;
        }
    }

    /* know operation, decode the mod byte to find the addressing
       mode. */
    if (mod < 3) {
        if (M.x86.mode & SYSMODE_PREFIX_DATA) {
            var destval u32

            DECODE_PRINTF("DWORD PTR ");
            destoffset = decode_rmXX_address(mod, rl);
            amt = fetch_byte_imm();
            DECODE_PRINTF2(",%x\n", amt);
            destval = fetch_data_long(destoffset);
            TRACE_AND_STEP();
            destval = (*opcD1_long_operation[rh]) (destval, amt);
            store_data_long(destoffset, destval);
        } else {
            var destval u16

            DECODE_PRINTF("WORD PTR ");
            destoffset = decode_rmXX_address(mod, rl);
            amt = fetch_byte_imm();
            DECODE_PRINTF2(",%x\n", amt);
            destval = fetch_data_word(destoffset);
            TRACE_AND_STEP();
            destval = (*opcD1_word_operation[rh]) (destval, amt);
            store_data_word(destoffset, destval);
        }
    } else { /* register to register */
        if (M.x86.mode & SYSMODE_PREFIX_DATA) {
            u32 *destreg;

            destreg = DECODE_RM_LONG_REGISTER(rl);
            amt = fetch_byte_imm();
            DECODE_PRINTF2(",%x\n", amt);
            TRACE_AND_STEP();
            *destreg = (*opcD1_long_operation[rh]) (*destreg, amt);
        } else {
            var destreg *u16

            destreg = DECODE_RM_WORD_REGISTER(rl);
            amt = fetch_byte_imm();
            DECODE_PRINTF2(",%x\n", amt);
            TRACE_AND_STEP();
            *destreg = (*opcD1_word_operation[rh]) (*destreg, amt);
        }
    }
    DECODE_CLEAR_SEGOVR();
    END_OF_INSTR();
}

/****************************************************************************
REMARKS:
Handles opcode 0xc2
****************************************************************************/
func x86emuOp_ret_near_IMM(_ u8) {
    u16 imm;

    START_OF_INSTR();
    DECODE_PRINTF("RET\t");
    imm = fetch_word_imm();
    DECODE_PRINTF2("%x\n", imm);
 TRACE_AND_STEP();
    M.x86.R_IP = pop_word();
 RETURN_TRACE(M.x86.saved_cs,M.x86.saved_ip, M.x86.R_CS, M.x86.R_IP, "NEAR");
    M.x86.R_SP += imm;
    DECODE_CLEAR_SEGOVR();
    END_OF_INSTR();
}

/****************************************************************************
REMARKS:
Handles opcode 0xc3
****************************************************************************/
func x86emuOp_ret_near(_ u8) {
    START_OF_INSTR();
    DECODE_PRINTF("RET\n");
 TRACE_AND_STEP();
    M.x86.R_IP = pop_word();
 RETURN_TRACE(M.x86.saved_cs,M.x86.saved_ip, M.x86.R_CS, M.x86.R_IP, "NEAR");
    DECODE_CLEAR_SEGOVR();
    END_OF_INSTR();
}

/****************************************************************************
REMARKS:
Handles opcode 0xc4
****************************************************************************/
func x86emuOp_les_R_IMM(_ u8) {
    int mod, rh, rl;
    u16 *dstreg;
    var srcoffset uint

    START_OF_INSTR();
    DECODE_PRINTF("LES\t");
    FETCH_DECODE_MODRM(mod, rh, rl);
    if (mod < 3) {
        dstreg = DECODE_RM_WORD_REGISTER(rh);
        DECODE_PRINTF(",");
        srcoffset = decode_rmXX_address(mod, rl);
        DECODE_PRINTF("\n");
        TRACE_AND_STEP();
        *dstreg = fetch_data_word(srcoffset);
        M.x86.R_ES = fetch_data_word(srcoffset + 2);
    }
    /* else UNDEFINED!                   register to register */

    DECODE_CLEAR_SEGOVR();
    END_OF_INSTR();
}

/****************************************************************************
REMARKS:
Handles opcode 0xc5
****************************************************************************/
func x86emuOp_lds_R_IMM(_ u8) {
    int mod, rh, rl;
    u16 *dstreg;
    var srcoffset uint

    START_OF_INSTR();
    DECODE_PRINTF("LDS\t");
    FETCH_DECODE_MODRM(mod, rh, rl);
    if (mod < 3) {
        dstreg = DECODE_RM_WORD_REGISTER(rh);
        DECODE_PRINTF(",");
        srcoffset = decode_rmXX_address(mod, rl);
        DECODE_PRINTF("\n");
        TRACE_AND_STEP();
        *dstreg = fetch_data_word(srcoffset);
        M.x86.R_DS = fetch_data_word(srcoffset + 2);
    }
    /* else UNDEFINED! */
    DECODE_CLEAR_SEGOVR();
    END_OF_INSTR();
}

/****************************************************************************
REMARKS:
Handles opcode 0xc6
****************************************************************************/
func x86emuOp_mov_byte_RM_IMM(_ u8) {
    var mod, rl, rh int
    u8 *destreg;
    var destoffset uint
    u8 imm;

    START_OF_INSTR();
    DECODE_PRINTF("MOV\t");
    FETCH_DECODE_MODRM(mod, rh, rl);
    if (rh != 0) {
        DECODE_PRINTF("ILLEGAL DECODE OF OPCODE c6\n");
        HALT_SYS();
    }
    if (mod < 3) {
        DECODE_PRINTF("BYTE PTR ");
        destoffset = decode_rmXX_address(mod, rl);
        imm = fetch_byte_imm();
        DECODE_PRINTF2(",%2x\n", imm);
        TRACE_AND_STEP();
        store_data_byte(destoffset, imm);
    } else { /* register to register */
        destreg = DECODE_RM_BYTE_REGISTER(rl);
        imm = fetch_byte_imm();
        DECODE_PRINTF2(",%2x\n", imm);
        TRACE_AND_STEP();
        *destreg = imm;
    }
    DECODE_CLEAR_SEGOVR();
    END_OF_INSTR();
}

/****************************************************************************
REMARKS:
Handles opcode 0xc7
****************************************************************************/
func x86emuOp_mov_word_RM_IMM(_ u8) {
    var mod, rl, rh int
    var destoffset uint

    START_OF_INSTR();
    DECODE_PRINTF("MOV\t");
    FETCH_DECODE_MODRM(mod, rh, rl);
    if (rh != 0) {
        DECODE_PRINTF("ILLEGAL DECODE OF OPCODE 8F\n");
        HALT_SYS();
    }
    if (mod < 3) {
        if (M.x86.mode & SYSMODE_PREFIX_DATA) {
            u32 imm;

            DECODE_PRINTF("DWORD PTR ");
            destoffset = decode_rmXX_address(mod, rl);
            imm = fetch_long_imm();
            DECODE_PRINTF2(",%x\n", imm);
            TRACE_AND_STEP();
            store_data_long(destoffset, imm);
        } else {
            u16 imm;

            DECODE_PRINTF("WORD PTR ");
            destoffset = decode_rmXX_address(mod, rl);
            imm = fetch_word_imm();
            DECODE_PRINTF2(",%x\n", imm);
            TRACE_AND_STEP();
            store_data_word(destoffset, imm);
        }
    } else { /* register to register */
        if (M.x86.mode & SYSMODE_PREFIX_DATA) {
   u32 *destreg;
   u32 imm;

            destreg = DECODE_RM_LONG_REGISTER(rl);
            imm = fetch_long_imm();
            DECODE_PRINTF2(",%x\n", imm);
            TRACE_AND_STEP();
            *destreg = imm;
        } else {
   var destreg *u16
   u16 imm;

            destreg = DECODE_RM_WORD_REGISTER(rl);
            imm = fetch_word_imm();
            DECODE_PRINTF2(",%x\n", imm);
            TRACE_AND_STEP();
            *destreg = imm;
        }
    }
    DECODE_CLEAR_SEGOVR();
    END_OF_INSTR();
}

/****************************************************************************
REMARKS:
Handles opcode 0xc8
****************************************************************************/
func x86emuOp_enter(_ u8) {
    u16 local,frame_pointer;
    u8 nesting;
    int i;

    START_OF_INSTR();
    local = fetch_word_imm();
    nesting = fetch_byte_imm();
    DECODE_PRINTF2("ENTER %x\n", local);
    DECODE_PRINTF2(",%x\n", nesting);
    TRACE_AND_STEP();
    push_word(M.x86.R_BP);
    frame_pointer = M.x86.R_SP;
    if (nesting > 0) {
        for (i = 1; i < nesting; i++) {
            M.x86.R_BP -= 2;
            push_word(fetch_data_word_abs(M.x86.R_SS, M.x86.R_BP));
            }
        push_word(frame_pointer);
        }
    M.x86.R_BP = frame_pointer;
    M.x86.R_SP = (u16)(M.x86.R_SP - local);
    DECODE_CLEAR_SEGOVR();
    END_OF_INSTR();
}

/****************************************************************************
REMARKS:
Handles opcode 0xc9
****************************************************************************/
func x86emuOp_leave(_ u8) {
    START_OF_INSTR();
    DECODE_PRINTF("LEAVE\n");
    TRACE_AND_STEP();
    M.x86.R_SP = M.x86.R_BP;
    M.x86.R_BP = pop_word();
    DECODE_CLEAR_SEGOVR();
    END_OF_INSTR();
}

/****************************************************************************
REMARKS:
Handles opcode 0xca
****************************************************************************/
func x86emuOp_ret_far_IMM(_ u8) {
    u16 imm;

    START_OF_INSTR();
    DECODE_PRINTF("RETF\t");
    imm = fetch_word_imm();
    DECODE_PRINTF2("%x\n", imm);
 TRACE_AND_STEP();
    M.x86.R_IP = pop_word();
    M.x86.R_CS = pop_word();
 RETURN_TRACE(M.x86.saved_cs,M.x86.saved_ip, M.x86.R_CS, M.x86.R_IP, "FAR");
    M.x86.R_SP += imm;
    DECODE_CLEAR_SEGOVR();
    END_OF_INSTR();
}

/****************************************************************************
REMARKS:
Handles opcode 0xcb
****************************************************************************/
func x86emuOp_ret_far(_ u8) {
    START_OF_INSTR();
    DECODE_PRINTF("RETF\n");
 TRACE_AND_STEP();
    M.x86.R_IP = pop_word();
    M.x86.R_CS = pop_word();
 RETURN_TRACE(M.x86.saved_cs,M.x86.saved_ip, M.x86.R_CS, M.x86.R_IP, "FAR");
    DECODE_CLEAR_SEGOVR();
    END_OF_INSTR();
}

/****************************************************************************
REMARKS:
Handles opcode 0xcc
****************************************************************************/
func x86emuOp_int3(_ u8) {
    u16 _;

    START_OF_INSTR();
    DECODE_PRINTF("INT 3\n");
    tmp = (u16) mem_access_word(3 * 4 + 2);
    /* access the segment register */
    TRACE_AND_STEP();
 if (_X86EMU_intrTab[3]) {
  (*_X86EMU_intrTab[3])(3);
    } else {
        push_word((u16)M.x86.R_FLG);
        CLEAR_FLAG(F_IF);
        CLEAR_FLAG(F_TF);
        push_word(M.x86.R_CS);
        M.x86.R_CS = mem_access_word(3 * 4 + 2);
        push_word(M.x86.R_IP);
        M.x86.R_IP = mem_access_word(3 * 4);
    }
    DECODE_CLEAR_SEGOVR();
    END_OF_INSTR();
}

/****************************************************************************
REMARKS:
Handles opcode 0xcd
****************************************************************************/
func x86emuOp_int_IMM(_ u8) {
    u16 _;
    u8 intnum;

    START_OF_INSTR();
    DECODE_PRINTF("INT\t");
    intnum = fetch_byte_imm();
    DECODE_PRINTF2("%x\n", intnum);
    tmp = mem_access_word(intnum * 4 + 2);
    TRACE_AND_STEP();
 if (_X86EMU_intrTab[intnum]) {
  (*_X86EMU_intrTab[intnum])(intnum);
    } else {
        push_word((u16)M.x86.R_FLG);
        CLEAR_FLAG(F_IF);
        CLEAR_FLAG(F_TF);
        push_word(M.x86.R_CS);
        M.x86.R_CS = mem_access_word(intnum * 4 + 2);
        push_word(M.x86.R_IP);
        M.x86.R_IP = mem_access_word(intnum * 4);
    }
    DECODE_CLEAR_SEGOVR();
    END_OF_INSTR();
}

/****************************************************************************
REMARKS:
Handles opcode 0xce
****************************************************************************/
func x86emuOp_into(_ u8) {
    u16 _;

    START_OF_INSTR();
    DECODE_PRINTF("INTO\n");
    TRACE_AND_STEP();
    if (ACCESS_FLAG(F_OF)) {
        tmp = mem_access_word(4 * 4 + 2);
  if (_X86EMU_intrTab[4]) {
   (*_X86EMU_intrTab[4])(4);
        } else {
            push_word((u16)M.x86.R_FLG);
            CLEAR_FLAG(F_IF);
            CLEAR_FLAG(F_TF);
            push_word(M.x86.R_CS);
            M.x86.R_CS = mem_access_word(4 * 4 + 2);
            push_word(M.x86.R_IP);
            M.x86.R_IP = mem_access_word(4 * 4);
        }
    }
    DECODE_CLEAR_SEGOVR();
    END_OF_INSTR();
}

/****************************************************************************
REMARKS:
Handles opcode 0xcf
****************************************************************************/
func x86emuOp_iret(_ u8) {
    START_OF_INSTR();
    DECODE_PRINTF("IRET\n");

    TRACE_AND_STEP();

    M.x86.R_IP = pop_word();
    M.x86.R_CS = pop_word();
    M.x86.R_FLG = pop_word();
    DECODE_CLEAR_SEGOVR();
    END_OF_INSTR();
}

/****************************************************************************
REMARKS:
Handles opcode 0xd0
****************************************************************************/
func x86emuOp_opcD0_byte_RM_1(_ u8) {
    var mod, rl, rh int
    u8 *destreg;
    var destoffset uint
    var destval u8

    /*
     * Yet another weirdo special case instruction format.  Part of
     * the opcode held below in "RH".  Doubly nested case would
     * result, except that the decoded instruction
     */
    START_OF_INSTR();
    FETCH_DECODE_MODRM(mod, rh, rl);

    if (DEBUG_DECODE()) {
        /* XXX DECODE_PRINTF may be changed to something more
           general, so that it is important to leave the strings
           in the same format, even though the result is that the
           above test is done twice. */
        switch (rh) {
        case 0:
            DECODE_PRINTF("ROL\t");
            break;
        case 1:
            DECODE_PRINTF("ROR\t");
            break;
        case 2:
            DECODE_PRINTF("RCL\t");
            break;
        case 3:
            DECODE_PRINTF("RCR\t");
            break;
        case 4:
            DECODE_PRINTF("SHL\t");
            break;
        case 5:
            DECODE_PRINTF("SHR\t");
            break;
        case 6:
            DECODE_PRINTF("SAL\t");
            break;
        case 7:
            DECODE_PRINTF("SAR\t");
            break;
        }
    }

    /* know operation, decode the mod byte to find the addressing
       mode. */
    if (mod < 3) {
        DECODE_PRINTF("BYTE PTR ");
        destoffset = decode_rmXX_address(mod, rl);
        DECODE_PRINTF(",1\n");
        destval = fetch_data_byte(destoffset);
        TRACE_AND_STEP();
        destval = (*opcD0_byte_operation[rh]) (destval, 1);
        store_data_byte(destoffset, destval);
    } else { /* register to register */
        destreg = DECODE_RM_BYTE_REGISTER(rl);
        DECODE_PRINTF(",1\n");
        TRACE_AND_STEP();
        destval = (*opcD0_byte_operation[rh]) (*destreg, 1);
        *destreg = destval;
    }
    DECODE_CLEAR_SEGOVR();
    END_OF_INSTR();
}

/****************************************************************************
REMARKS:
Handles opcode 0xd1
****************************************************************************/
func x86emuOp_opcD1_word_RM_1(_ u8) {
    var mod, rl, rh int
    var destoffset uint

    /*
     * Yet another weirdo special case instruction format.  Part of
     * the opcode held below in "RH".  Doubly nested case would
     * result, except that the decoded instruction
     */
    START_OF_INSTR();
    FETCH_DECODE_MODRM(mod, rh, rl);

    if (DEBUG_DECODE()) {
        /* XXX DECODE_PRINTF may be changed to something more
           general, so that it is important to leave the strings
           in the same format, even though the result is that the
           above test is done twice. */
        switch (rh) {
        case 0:
            DECODE_PRINTF("ROL\t");
            break;
        case 1:
            DECODE_PRINTF("ROR\t");
            break;
        case 2:
            DECODE_PRINTF("RCL\t");
            break;
        case 3:
            DECODE_PRINTF("RCR\t");
            break;
        case 4:
            DECODE_PRINTF("SHL\t");
            break;
        case 5:
            DECODE_PRINTF("SHR\t");
            break;
        case 6:
            DECODE_PRINTF("SAL\t");
            break;
        case 7:
            DECODE_PRINTF("SAR\t");
            break;
        }
    }

    /* know operation, decode the mod byte to find the addressing
       mode. */
    if (mod < 3) {
        if (M.x86.mode & SYSMODE_PREFIX_DATA) {
            var destval u32

            DECODE_PRINTF("DWORD PTR ");
            destoffset = decode_rmXX_address(mod, rl);
            DECODE_PRINTF(",1\n");
            destval = fetch_data_long(destoffset);
            TRACE_AND_STEP();
            destval = (*opcD1_long_operation[rh]) (destval, 1);
            store_data_long(destoffset, destval);
        } else {
            var destval u16

            DECODE_PRINTF("WORD PTR ");
            destoffset = decode_rmXX_address(mod, rl);
            DECODE_PRINTF(",1\n");
            destval = fetch_data_word(destoffset);
            TRACE_AND_STEP();
            destval = (*opcD1_word_operation[rh]) (destval, 1);
            store_data_word(destoffset, destval);
        }
    } else { /* register to register */
        if (M.x86.mode & SYSMODE_PREFIX_DATA) {
   var destval u32
   u32 *destreg;

            destreg = DECODE_RM_LONG_REGISTER(rl);
            DECODE_PRINTF(",1\n");
            TRACE_AND_STEP();
            destval = (*opcD1_long_operation[rh]) (*destreg, 1);
            *destreg = destval;
        } else {
   var destval u16
   var destreg *u16

            destreg = DECODE_RM_WORD_REGISTER(rl);
            DECODE_PRINTF(",1\n");
            TRACE_AND_STEP();
            destval = (*opcD1_word_operation[rh]) (*destreg, 1);
            *destreg = destval;
        }
    }
    DECODE_CLEAR_SEGOVR();
    END_OF_INSTR();
}

/****************************************************************************
REMARKS:
Handles opcode 0xd2
****************************************************************************/
func x86emuOp_opcD2_byte_RM_CL(_ u8) {
    var mod, rl, rh int
    u8 *destreg;
    var destoffset uint
    var destval u8
    u8 amt;

    /*
     * Yet another weirdo special case instruction format.  Part of
     * the opcode held below in "RH".  Doubly nested case would
     * result, except that the decoded instruction
     */
    START_OF_INSTR();
    FETCH_DECODE_MODRM(mod, rh, rl);

    if (DEBUG_DECODE()) {
        /* XXX DECODE_PRINTF may be changed to something more
           general, so that it is important to leave the strings
           in the same format, even though the result is that the
           above test is done twice. */
        switch (rh) {
        case 0:
            DECODE_PRINTF("ROL\t");
            break;
        case 1:
            DECODE_PRINTF("ROR\t");
            break;
        case 2:
            DECODE_PRINTF("RCL\t");
            break;
        case 3:
            DECODE_PRINTF("RCR\t");
            break;
        case 4:
            DECODE_PRINTF("SHL\t");
            break;
        case 5:
            DECODE_PRINTF("SHR\t");
            break;
        case 6:
            DECODE_PRINTF("SAL\t");
            break;
        case 7:
            DECODE_PRINTF("SAR\t");
            break;
        }
    }

    /* know operation, decode the mod byte to find the addressing
       mode. */
    amt = M.x86.R_CL;
    if (mod < 3) {
        DECODE_PRINTF("BYTE PTR ");
        destoffset = decode_rmXX_address(mod, rl);
        DECODE_PRINTF(",CL\n");
        destval = fetch_data_byte(destoffset);
        TRACE_AND_STEP();
        destval = (*opcD0_byte_operation[rh]) (destval, amt);
        store_data_byte(destoffset, destval);
    } else { /* register to register */
        destreg = DECODE_RM_BYTE_REGISTER(rl);
        DECODE_PRINTF(",CL\n");
        TRACE_AND_STEP();
        destval = (*opcD0_byte_operation[rh]) (*destreg, amt);
        *destreg = destval;
    }
    DECODE_CLEAR_SEGOVR();
    END_OF_INSTR();
}

/****************************************************************************
REMARKS:
Handles opcode 0xd3
****************************************************************************/
func x86emuOp_opcD3_word_RM_CL(_ u8) {
    var mod, rl, rh int
    var destoffset uint
    u8 amt;

    /*
     * Yet another weirdo special case instruction format.  Part of
     * the opcode held below in "RH".  Doubly nested case would
     * result, except that the decoded instruction
     */
    START_OF_INSTR();
    FETCH_DECODE_MODRM(mod, rh, rl);

    if (DEBUG_DECODE()) {
        /* XXX DECODE_PRINTF may be changed to something more
           general, so that it is important to leave the strings
           in the same format, even though the result is that the
           above test is done twice. */
        switch (rh) {
        case 0:
            DECODE_PRINTF("ROL\t");
            break;
        case 1:
            DECODE_PRINTF("ROR\t");
            break;
        case 2:
            DECODE_PRINTF("RCL\t");
            break;
        case 3:
            DECODE_PRINTF("RCR\t");
            break;
        case 4:
            DECODE_PRINTF("SHL\t");
            break;
        case 5:
            DECODE_PRINTF("SHR\t");
            break;
        case 6:
            DECODE_PRINTF("SAL\t");
            break;
        case 7:
            DECODE_PRINTF("SAR\t");
            break;
        }
    }

    /* know operation, decode the mod byte to find the addressing
       mode. */
    amt = M.x86.R_CL;
    if (mod < 3) {
        if (M.x86.mode & SYSMODE_PREFIX_DATA) {
            var destval u32

            DECODE_PRINTF("DWORD PTR ");
            destoffset = decode_rmXX_address(mod, rl);
            DECODE_PRINTF(",CL\n");
            destval = fetch_data_long(destoffset);
            TRACE_AND_STEP();
            destval = (*opcD1_long_operation[rh]) (destval, amt);
            store_data_long(destoffset, destval);
        } else {
            var destval u16

            DECODE_PRINTF("WORD PTR ");
            destoffset = decode_rmXX_address(mod, rl);
            DECODE_PRINTF(",CL\n");
            destval = fetch_data_word(destoffset);
            TRACE_AND_STEP();
            destval = (*opcD1_word_operation[rh]) (destval, amt);
            store_data_word(destoffset, destval);
        }
    } else { /* register to register */
        if (M.x86.mode & SYSMODE_PREFIX_DATA) {
            u32 *destreg;

            destreg = DECODE_RM_LONG_REGISTER(rl);
            DECODE_PRINTF(",CL\n");
            TRACE_AND_STEP();
            *destreg = (*opcD1_long_operation[rh]) (*destreg, amt);
        } else {
            var destreg *u16

            destreg = DECODE_RM_WORD_REGISTER(rl);
            DECODE_PRINTF(",CL\n");
            TRACE_AND_STEP();
            *destreg = (*opcD1_word_operation[rh]) (*destreg, amt);
        }
    }
    DECODE_CLEAR_SEGOVR();
    END_OF_INSTR();
}

/****************************************************************************
REMARKS:
Handles opcode 0xd4
****************************************************************************/
func x86emuOp_aam(_ u8) {
    u8 a;

    START_OF_INSTR();
    DECODE_PRINTF("AAM\n");
    a = fetch_byte_imm(); /* this is a stupid encoding. */
    if (a != 10) {
        DECODE_PRINTF("ERROR DECODING AAM\n");
        TRACE_REGS();
        HALT_SYS();
    }
    TRACE_AND_STEP();
    /* note the type change here --- returning AL and AH in AX. */
    M.x86.R_AX = aam_word(M.x86.R_AL);
    DECODE_CLEAR_SEGOVR();
    END_OF_INSTR();
}

/****************************************************************************
REMARKS:
Handles opcode 0xd5
****************************************************************************/
func x86emuOp_aad(_ u8) {
    u8 _;

    START_OF_INSTR();
    DECODE_PRINTF("AAD\n");
    a = fetch_byte_imm();
    TRACE_AND_STEP();
    M.x86.R_AX = aad_word(M.x86.R_AX);
    DECODE_CLEAR_SEGOVR();
    END_OF_INSTR();
}

/* opcode 0xd6 ILLEGAL OPCODE */

/****************************************************************************
REMARKS:
Handles opcode 0xd7
****************************************************************************/
func x86emuOp_xlat(_ u8) {
    u16 addr;

    START_OF_INSTR();
    DECODE_PRINTF("XLAT\n");
    TRACE_AND_STEP();
 addr = (u16)(M.x86.R_BX + (u8)M.x86.R_AL);
    M.x86.R_AL = fetch_data_byte(addr);
    DECODE_CLEAR_SEGOVR();
    END_OF_INSTR();
}

/* Instructions  D8 .. DF are in i87_ops.c */

/****************************************************************************
REMARKS:
Handles opcode 0xe0
****************************************************************************/
func x86emuOp_loopne(_ u8) {
    s16 ip;

    START_OF_INSTR();
    DECODE_PRINTF("LOOPNE\t");
    ip = (s8) fetch_byte_imm();
    ip += (s16) M.x86.R_IP;
    DECODE_PRINTF2("%04x\n", ip);
    TRACE_AND_STEP();
    if (M.x86.mode & SYSMODE_PREFIX_ADDR)
        M.x86.R_ECX -= 1;
    else
        M.x86.R_CX -= 1;
    if (((M.x86.mode & SYSMODE_PREFIX_ADDR) ? M.x86.R_ECX : M.x86.R_CX) != 0 && !ACCESS_FLAG(F_ZF)) /* (E)CX != 0 and !ZF */
        M.x86.R_IP = ip;
    DECODE_CLEAR_SEGOVR();
    END_OF_INSTR();
}

/****************************************************************************
REMARKS:
Handles opcode 0xe1
****************************************************************************/
func x86emuOp_loope(_ u8) {
    s16 ip;

    START_OF_INSTR();
    DECODE_PRINTF("LOOPE\t");
    ip = (s8) fetch_byte_imm();
    ip += (s16) M.x86.R_IP;
    DECODE_PRINTF2("%04x\n", ip);
    TRACE_AND_STEP();
    if (M.x86.mode & SYSMODE_PREFIX_ADDR)
        M.x86.R_ECX -= 1;
    else
        M.x86.R_CX -= 1;
    if (((M.x86.mode & SYSMODE_PREFIX_ADDR) ? M.x86.R_ECX : M.x86.R_CX) != 0 && ACCESS_FLAG(F_ZF)) /* (E)CX != 0 and ZF */
        M.x86.R_IP = ip;
    DECODE_CLEAR_SEGOVR();
    END_OF_INSTR();
}

/****************************************************************************
REMARKS:
Handles opcode 0xe2
****************************************************************************/
func x86emuOp_loop(_ u8) {
    s16 ip;

    START_OF_INSTR();
    DECODE_PRINTF("LOOP\t");
    ip = (s8) fetch_byte_imm();
    ip += (s16) M.x86.R_IP;
    DECODE_PRINTF2("%04x\n", ip);
    TRACE_AND_STEP();
    if (M.x86.mode & SYSMODE_PREFIX_ADDR)
        M.x86.R_ECX -= 1;
    else
        M.x86.R_CX -= 1;
    if (((M.x86.mode & SYSMODE_PREFIX_ADDR) ? M.x86.R_ECX : M.x86.R_CX) != 0) /* (E)CX != 0 */
        M.x86.R_IP = ip;
    DECODE_CLEAR_SEGOVR();
    END_OF_INSTR();
}

/****************************************************************************
REMARKS:
Handles opcode 0xe3
****************************************************************************/
func x86emuOp_jcxz(_ u8) {
    u16 target;
    s8 offset;

    /* jump to byte offset if overflow flag is set */
    START_OF_INSTR();
    DECODE_PRINTF("JCXZ\t");
    offset = (s8)fetch_byte_imm();
    target = (u16)(M.x86.R_IP + offset);
    DECODE_PRINTF2("%x\n", target);
    TRACE_AND_STEP();
    if (M.x86.R_CX == 0) {
        M.x86.R_IP = target;
 JMP_TRACE(M.x86.saved_cs, M.x86.saved_ip, M.x86.R_CS, M.x86.R_IP, " CXZ ");
    }
    DECODE_CLEAR_SEGOVR();
    END_OF_INSTR();
}

/****************************************************************************
REMARKS:
Handles opcode 0xe4
****************************************************************************/
func x86emuOp_in_byte_AL_IMM(_ u8) {
    u8 port;

    START_OF_INSTR();
    DECODE_PRINTF("IN\t");
 port = (u8) fetch_byte_imm();
    DECODE_PRINTF2("%x,AL\n", port);
    TRACE_AND_STEP();
    M.x86.R_AL = sys_inb(port);
    DECODE_CLEAR_SEGOVR();
    END_OF_INSTR();
}

/****************************************************************************
REMARKS:
Handles opcode 0xe5
****************************************************************************/
func x86emuOp_in_word_AX_IMM(_ u8) {
    u8 port;

    START_OF_INSTR();
    DECODE_PRINTF("IN\t");
 port = (u8) fetch_byte_imm();
    if (M.x86.mode & SYSMODE_PREFIX_DATA) {
        DECODE_PRINTF2("EAX,%x\n", port);
    } else {
        DECODE_PRINTF2("AX,%x\n", port);
    }
    TRACE_AND_STEP();
    if (M.x86.mode & SYSMODE_PREFIX_DATA) {
        M.x86.R_EAX = sys_inl(port);
    } else {
        M.x86.R_AX = sys_inw(port);
    }
    DECODE_CLEAR_SEGOVR();
    END_OF_INSTR();
}

/****************************************************************************
REMARKS:
Handles opcode 0xe6
****************************************************************************/
func x86emuOp_out_byte_IMM_AL(_ u8) {
    u8 port;

    START_OF_INSTR();
    DECODE_PRINTF("OUT\t");
 port = (u8) fetch_byte_imm();
    DECODE_PRINTF2("%x,AL\n", port);
    TRACE_AND_STEP();
    sys_outb(port, M.x86.R_AL);
    DECODE_CLEAR_SEGOVR();
    END_OF_INSTR();
}

/****************************************************************************
REMARKS:
Handles opcode 0xe7
****************************************************************************/
func x86emuOp_out_word_IMM_AX(_ u8) {
    u8 port;

    START_OF_INSTR();
    DECODE_PRINTF("OUT\t");
 port = (u8) fetch_byte_imm();
    if (M.x86.mode & SYSMODE_PREFIX_DATA) {
        DECODE_PRINTF2("%x,EAX\n", port);
    } else {
        DECODE_PRINTF2("%x,AX\n", port);
    }
    TRACE_AND_STEP();
    if (M.x86.mode & SYSMODE_PREFIX_DATA) {
        sys_outl(port, M.x86.R_EAX);
    } else {
        sys_outw(port, M.x86.R_AX);
    }
    DECODE_CLEAR_SEGOVR();
    END_OF_INSTR();
}

/****************************************************************************
REMARKS:
Handles opcode 0xe8
****************************************************************************/
func x86emuOp_call_near_IMM(_ u8) {
    s16 ip16 = 0; /* Initialize to keep GCC silent */
    s32 ip32 = 0;

    START_OF_INSTR();
    DECODE_PRINTF("CALL\t");
    if (M.x86.mode & SYSMODE_PREFIX_DATA) {
        ip32 = (s32) fetch_long_imm();
        ip32 += (s16) M.x86.R_IP; /* CHECK SIGN */
        DECODE_PRINTF2("%04x\n", (u16)ip32);
        CALL_TRACE(M.x86.saved_cs, M.x86.saved_ip, M.x86.R_CS, ip32, "");
    } else {
        ip16 = (s16) fetch_word_imm();
        ip16 += (s16) M.x86.R_IP; /* CHECK SIGN */
        DECODE_PRINTF2("%04x\n", ip16);
        CALL_TRACE(M.x86.saved_cs, M.x86.saved_ip, M.x86.R_CS, ip16, "");
    }
    TRACE_AND_STEP();
    if (M.x86.mode & SYSMODE_PREFIX_DATA) {
        push_long(M.x86.R_EIP);
        M.x86.R_EIP = ip32 & 0xffff;
    } else {
        push_word(M.x86.R_IP);
        M.x86.R_EIP = ip16;
    }
    DECODE_CLEAR_SEGOVR();
    END_OF_INSTR();
}

/****************************************************************************
REMARKS:
Handles opcode 0xe9
****************************************************************************/
func x86emuOp_jump_near_IMM(_ u8) {
    u32 ip;

    START_OF_INSTR();
    DECODE_PRINTF("JMP\t");
    if (M.x86.mode & SYSMODE_PREFIX_DATA) {
        ip = (u32)fetch_long_imm();
 ip += (u32)M.x86.R_EIP;
 DECODE_PRINTF2("%08x\n", (u32)ip);
        JMP_TRACE(M.x86.saved_cs, M.x86.saved_ip, M.x86.R_CS, ip, " NEAR ");
 TRACE_AND_STEP();
 M.x86.R_EIP = (u32)ip;
    } else {
        ip = (s16)fetch_word_imm();
        ip += (s16)M.x86.R_IP;
        DECODE_PRINTF2("%04x\n", (u16)ip);
        JMP_TRACE(M.x86.saved_cs, M.x86.saved_ip, M.x86.R_CS, ip, " NEAR ");
        TRACE_AND_STEP();
        M.x86.R_IP = (u16)ip;
    }
    DECODE_CLEAR_SEGOVR();
    END_OF_INSTR();
}

/****************************************************************************
REMARKS:
Handles opcode 0xea
****************************************************************************/
func x86emuOp_jump_far_IMM(_ u8) {
    u16 cs;
    u32 ip;

    START_OF_INSTR();
    DECODE_PRINTF("JMP\tFAR ");
    if (M.x86.mode & SYSMODE_PREFIX_DATA) {
        ip = fetch_long_imm();
    } else {
        ip = fetch_word_imm();
    }
    cs = fetch_word_imm();
    DECODE_PRINTF2("%04x:", cs);
    DECODE_PRINTF2("%04x\n", ip);
    JMP_TRACE(M.x86.saved_cs, M.x86.saved_ip, cs, ip, " FAR ");
    TRACE_AND_STEP();
    M.x86.R_EIP = ip & 0xffff;
    M.x86.R_CS = cs;
    DECODE_CLEAR_SEGOVR();
    END_OF_INSTR();
}

/****************************************************************************
REMARKS:
Handles opcode 0xeb
****************************************************************************/
func x86emuOp_jump_byte_IMM(_ u8) {
    u16 target;
    s8 offset;

    START_OF_INSTR();
    DECODE_PRINTF("JMP\t");
    offset = (s8)fetch_byte_imm();
    target = (u16)(M.x86.R_IP + offset);
    DECODE_PRINTF2("%x\n", target);
    JMP_TRACE(M.x86.saved_cs, M.x86.saved_ip, M.x86.R_CS, target, " BYTE ");
    TRACE_AND_STEP();
    M.x86.R_IP = target;
    DECODE_CLEAR_SEGOVR();
    END_OF_INSTR();
}

/****************************************************************************
REMARKS:
Handles opcode 0xec
****************************************************************************/
func x86emuOp_in_byte_AL_DX(_ u8) {
    START_OF_INSTR();
    DECODE_PRINTF("IN\tAL,DX\n");
    TRACE_AND_STEP();
    M.x86.R_AL = sys_inb(M.x86.R_DX);
    DECODE_CLEAR_SEGOVR();
    END_OF_INSTR();
}

/****************************************************************************
REMARKS:
Handles opcode 0xed
****************************************************************************/
func x86emuOp_in_word_AX_DX(_ u8) {
    START_OF_INSTR();
    if (M.x86.mode & SYSMODE_PREFIX_DATA) {
        DECODE_PRINTF("IN\tEAX,DX\n");
    } else {
        DECODE_PRINTF("IN\tAX,DX\n");
    }
    TRACE_AND_STEP();
    if (M.x86.mode & SYSMODE_PREFIX_DATA) {
        M.x86.R_EAX = sys_inl(M.x86.R_DX);
    } else {
        M.x86.R_AX = sys_inw(M.x86.R_DX);
    }
    DECODE_CLEAR_SEGOVR();
    END_OF_INSTR();
}

/****************************************************************************
REMARKS:
Handles opcode 0xee
****************************************************************************/
func x86emuOp_out_byte_DX_AL(_ u8) {
    START_OF_INSTR();
    DECODE_PRINTF("OUT\tDX,AL\n");
    TRACE_AND_STEP();
    sys_outb(M.x86.R_DX, M.x86.R_AL);
    DECODE_CLEAR_SEGOVR();
    END_OF_INSTR();
}

/****************************************************************************
REMARKS:
Handles opcode 0xef
****************************************************************************/
func x86emuOp_out_word_DX_AX(_ u8) {
    START_OF_INSTR();
    if (M.x86.mode & SYSMODE_PREFIX_DATA) {
        DECODE_PRINTF("OUT\tDX,EAX\n");
    } else {
        DECODE_PRINTF("OUT\tDX,AX\n");
    }
    TRACE_AND_STEP();
    if (M.x86.mode & SYSMODE_PREFIX_DATA) {
        sys_outl(M.x86.R_DX, M.x86.R_EAX);
    } else {
        sys_outw(M.x86.R_DX, M.x86.R_AX);
    }
    DECODE_CLEAR_SEGOVR();
    END_OF_INSTR();
}

/****************************************************************************
REMARKS:
Handles opcode 0xf0
****************************************************************************/
func x86emuOp_lock(_ u8) {
    START_OF_INSTR();
    DECODE_PRINTF("LOCK:\n");
    TRACE_AND_STEP();
    DECODE_CLEAR_SEGOVR();
    END_OF_INSTR();
}

/*opcode 0xf1 ILLEGAL OPERATION */

/****************************************************************************
REMARKS:
Handles opcode 0xf2
****************************************************************************/
func x86emuOp_repne(_ u8) {
    START_OF_INSTR();
    DECODE_PRINTF("REPNE\n");
    TRACE_AND_STEP();
    M.x86.mode |= SYSMODE_PREFIX_REPNE;
    if (M.x86.mode & SYSMODE_PREFIX_ADDR)
        M.x86.mode |= SYSMODE_32BIT_REP;
    DECODE_CLEAR_SEGOVR();
    END_OF_INSTR();
}

/****************************************************************************
REMARKS:
Handles opcode 0xf3
****************************************************************************/
func x86emuOp_repe(_ u8) {
    START_OF_INSTR();
    DECODE_PRINTF("REPE\n");
    TRACE_AND_STEP();
    M.x86.mode |= SYSMODE_PREFIX_REPE;
    if (M.x86.mode & SYSMODE_PREFIX_ADDR)
        M.x86.mode |= SYSMODE_32BIT_REP;
    DECODE_CLEAR_SEGOVR();
    END_OF_INSTR();
}

/****************************************************************************
REMARKS:
Handles opcode 0xf4
****************************************************************************/
func x86emuOp_halt(_ u8) {
    START_OF_INSTR();
    DECODE_PRINTF("HALT\n");
    TRACE_AND_STEP();
    HALT_SYS();
    DECODE_CLEAR_SEGOVR();
    END_OF_INSTR();
}

/****************************************************************************
REMARKS:
Handles opcode 0xf5
****************************************************************************/
func x86emuOp_cmc(_ u8) {
    /* complement the carry flag. */
    START_OF_INSTR();
    DECODE_PRINTF("CMC\n");
    TRACE_AND_STEP();
    TOGGLE_FLAG(F_CF);
    DECODE_CLEAR_SEGOVR();
    END_OF_INSTR();
}

/****************************************************************************
REMARKS:
Handles opcode 0xf6
****************************************************************************/
func x86emuOp_opcF6_byte_RM(_ u8) {
    var mod, rl, rh int
    u8 *destreg;
    var destoffset uint
    u8 destval, srcval;

    /* long, drawn out code follows.  Double switch for a total
       of 32 cases.  */
    START_OF_INSTR();
    FETCH_DECODE_MODRM(mod, rh, rl);
    DECODE_PRINTF(opF6_names[rh]);
    if (mod < 3) {
        DECODE_PRINTF("BYTE PTR ");
        destoffset = decode_rmXX_address(mod, rl);
        destval = fetch_data_byte(destoffset);

        switch (rh) {
        case 0: /* test byte imm */
            DECODE_PRINTF(",");
            srcval = fetch_byte_imm();
            DECODE_PRINTF2("%02x\n", srcval);
            TRACE_AND_STEP();
            test_byte(destval, srcval);
            break;
        case 1:
            DECODE_PRINTF("ILLEGAL OP MOD=00 RH=01 OP=F6\n");
            HALT_SYS();
            break;
        case 2:
            DECODE_PRINTF("\n");
            TRACE_AND_STEP();
            destval = not_byte(destval);
            store_data_byte(destoffset, destval);
            break;
        case 3:
            DECODE_PRINTF("\n");
            TRACE_AND_STEP();
            destval = neg_byte(destval);
            store_data_byte(destoffset, destval);
            break;
        case 4:
            DECODE_PRINTF("\n");
            TRACE_AND_STEP();
            mul_byte(destval);
            break;
        case 5:
            DECODE_PRINTF("\n");
            TRACE_AND_STEP();
            imul_byte(destval);
            break;
        case 6:
            DECODE_PRINTF("\n");
            TRACE_AND_STEP();
            div_byte(destval);
            break;
        default:
            DECODE_PRINTF("\n");
            TRACE_AND_STEP();
            idiv_byte(destval);
            break;
        }
    } else { /* mod=11 */
        destreg = DECODE_RM_BYTE_REGISTER(rl);
        switch (rh) {
        case 0: /* test byte imm */
            DECODE_PRINTF(",");
            srcval = fetch_byte_imm();
            DECODE_PRINTF2("%02x\n", srcval);
            TRACE_AND_STEP();
            test_byte(*destreg, srcval);
            break;
        case 1:
            DECODE_PRINTF("ILLEGAL OP MOD=00 RH=01 OP=F6\n");
            HALT_SYS();
            break;
        case 2:
            DECODE_PRINTF("\n");
            TRACE_AND_STEP();
            *destreg = not_byte(*destreg);
            break;
        case 3:
            DECODE_PRINTF("\n");
            TRACE_AND_STEP();
            *destreg = neg_byte(*destreg);
            break;
        case 4:
            DECODE_PRINTF("\n");
            TRACE_AND_STEP();
            mul_byte(*destreg); /*!!!  */
            break;
        case 5:
            DECODE_PRINTF("\n");
            TRACE_AND_STEP();
            imul_byte(*destreg);
            break;
        case 6:
            DECODE_PRINTF("\n");
            TRACE_AND_STEP();
            div_byte(*destreg);
            break;
        default:
            DECODE_PRINTF("\n");
            TRACE_AND_STEP();
            idiv_byte(*destreg);
            break;
        }
    }
    DECODE_CLEAR_SEGOVR();
    END_OF_INSTR();
}

/****************************************************************************
REMARKS:
Handles opcode 0xf7
****************************************************************************/
func x86emuOp_opcF7_word_RM(_ u8) {
    var mod, rl, rh int
    var destoffset uint

    START_OF_INSTR();
    FETCH_DECODE_MODRM(mod, rh, rl);
    DECODE_PRINTF(opF6_names[rh]);
    if (mod < 3) {

        if (M.x86.mode & SYSMODE_PREFIX_DATA) {
            u32 destval, srcval;

            DECODE_PRINTF("DWORD PTR ");
            destoffset = decode_rmXX_address(mod, rl);
            destval = fetch_data_long(destoffset);

            switch (rh) {
            case 0:
                DECODE_PRINTF(",");
                srcval = fetch_long_imm();
                DECODE_PRINTF2("%x\n", srcval);
                TRACE_AND_STEP();
                test_long(destval, srcval);
                break;
            case 1:
                DECODE_PRINTF("ILLEGAL OP MOD=00 RH=01 OP=F7\n");
                HALT_SYS();
                break;
            case 2:
                DECODE_PRINTF("\n");
                TRACE_AND_STEP();
                destval = not_long(destval);
                store_data_long(destoffset, destval);
                break;
            case 3:
                DECODE_PRINTF("\n");
                TRACE_AND_STEP();
                destval = neg_long(destval);
                store_data_long(destoffset, destval);
                break;
            case 4:
                DECODE_PRINTF("\n");
                TRACE_AND_STEP();
                mul_long(destval);
                break;
            case 5:
                DECODE_PRINTF("\n");
                TRACE_AND_STEP();
                imul_long(destval);
                break;
            case 6:
                DECODE_PRINTF("\n");
                TRACE_AND_STEP();
                div_long(destval);
                break;
            case 7:
                DECODE_PRINTF("\n");
                TRACE_AND_STEP();
                idiv_long(destval);
                break;
            }
        } else {
            u16 destval, srcval;

            DECODE_PRINTF("WORD PTR ");
            destoffset = decode_rmXX_address(mod, rl);
            destval = fetch_data_word(destoffset);

            switch (rh) {
            case 0: /* test word imm */
                DECODE_PRINTF(",");
                srcval = fetch_word_imm();
                DECODE_PRINTF2("%x\n", srcval);
                TRACE_AND_STEP();
                test_word(destval, srcval);
                break;
            case 1:
                DECODE_PRINTF("ILLEGAL OP MOD=00 RH=01 OP=F7\n");
                HALT_SYS();
                break;
            case 2:
                DECODE_PRINTF("\n");
                TRACE_AND_STEP();
                destval = not_word(destval);
                store_data_word(destoffset, destval);
                break;
            case 3:
                DECODE_PRINTF("\n");
                TRACE_AND_STEP();
                destval = neg_word(destval);
                store_data_word(destoffset, destval);
                break;
            case 4:
                DECODE_PRINTF("\n");
                TRACE_AND_STEP();
                mul_word(destval);
                break;
            case 5:
                DECODE_PRINTF("\n");
                TRACE_AND_STEP();
                imul_word(destval);
                break;
            case 6:
                DECODE_PRINTF("\n");
                TRACE_AND_STEP();
                div_word(destval);
                break;
            case 7:
                DECODE_PRINTF("\n");
                TRACE_AND_STEP();
                idiv_word(destval);
                break;
            }
        }

    } else { /* mod=11 */

        if (M.x86.mode & SYSMODE_PREFIX_DATA) {
            u32 *destreg;
            var srcval u32

            destreg = DECODE_RM_LONG_REGISTER(rl);

            switch (rh) {
            case 0: /* test word imm */
                DECODE_PRINTF(",");
                srcval = fetch_long_imm();
                DECODE_PRINTF2("%x\n", srcval);
                TRACE_AND_STEP();
                test_long(*destreg, srcval);
                break;
            case 1:
                DECODE_PRINTF("ILLEGAL OP MOD=00 RH=01 OP=F6\n");
                HALT_SYS();
                break;
            case 2:
                DECODE_PRINTF("\n");
                TRACE_AND_STEP();
                *destreg = not_long(*destreg);
                break;
            case 3:
                DECODE_PRINTF("\n");
                TRACE_AND_STEP();
                *destreg = neg_long(*destreg);
                break;
            case 4:
                DECODE_PRINTF("\n");
                TRACE_AND_STEP();
                mul_long(*destreg); /*!!!  */
                break;
            case 5:
                DECODE_PRINTF("\n");
                TRACE_AND_STEP();
                imul_long(*destreg);
                break;
            case 6:
                DECODE_PRINTF("\n");
                TRACE_AND_STEP();
                div_long(*destreg);
                break;
            case 7:
                DECODE_PRINTF("\n");
                TRACE_AND_STEP();
                idiv_long(*destreg);
                break;
            }
        } else {
            var destreg *u16
            u16 srcval;

            destreg = DECODE_RM_WORD_REGISTER(rl);

            switch (rh) {
            case 0: /* test word imm */
                DECODE_PRINTF(",");
                srcval = fetch_word_imm();
                DECODE_PRINTF2("%x\n", srcval);
                TRACE_AND_STEP();
                test_word(*destreg, srcval);
                break;
            case 1:
                DECODE_PRINTF("ILLEGAL OP MOD=00 RH=01 OP=F6\n");
                HALT_SYS();
                break;
            case 2:
                DECODE_PRINTF("\n");
                TRACE_AND_STEP();
                *destreg = not_word(*destreg);
                break;
            case 3:
                DECODE_PRINTF("\n");
                TRACE_AND_STEP();
                *destreg = neg_word(*destreg);
                break;
            case 4:
                DECODE_PRINTF("\n");
                TRACE_AND_STEP();
                mul_word(*destreg); /*!!!  */
                break;
            case 5:
                DECODE_PRINTF("\n");
                TRACE_AND_STEP();
                imul_word(*destreg);
                break;
            case 6:
                DECODE_PRINTF("\n");
                TRACE_AND_STEP();
                div_word(*destreg);
                break;
            case 7:
                DECODE_PRINTF("\n");
                TRACE_AND_STEP();
                idiv_word(*destreg);
                break;
            }
        }
    }
    DECODE_CLEAR_SEGOVR();
    END_OF_INSTR();
}

/****************************************************************************
REMARKS:
Handles opcode 0xf8
****************************************************************************/
func x86emuOp_clc(_ u8) {
    /* clear the carry flag. */
    START_OF_INSTR();
    DECODE_PRINTF("CLC\n");
    TRACE_AND_STEP();
    CLEAR_FLAG(F_CF);
    DECODE_CLEAR_SEGOVR();
    END_OF_INSTR();
}

/****************************************************************************
REMARKS:
Handles opcode 0xf9
****************************************************************************/
func x86emuOp_stc(_ u8) {
    /* set the carry flag. */
    START_OF_INSTR();
    DECODE_PRINTF("STC\n");
    TRACE_AND_STEP();
    SET_FLAG(F_CF);
    DECODE_CLEAR_SEGOVR();
    END_OF_INSTR();
}

/****************************************************************************
REMARKS:
Handles opcode 0xfa
****************************************************************************/
func x86emuOp_cli(_ u8) {
    /* clear interrupts. */
    START_OF_INSTR();
    DECODE_PRINTF("CLI\n");
    TRACE_AND_STEP();
    CLEAR_FLAG(F_IF);
    DECODE_CLEAR_SEGOVR();
    END_OF_INSTR();
}

/****************************************************************************
REMARKS:
Handles opcode 0xfb
****************************************************************************/
func x86emuOp_sti(_ u8) {
    /* enable  interrupts. */
    START_OF_INSTR();
    DECODE_PRINTF("STI\n");
    TRACE_AND_STEP();
    SET_FLAG(F_IF);
    DECODE_CLEAR_SEGOVR();
    END_OF_INSTR();
}

/****************************************************************************
REMARKS:
Handles opcode 0xfc
****************************************************************************/
func x86emuOp_cld(_ u8) {
    /* clear interrupts. */
    START_OF_INSTR();
    DECODE_PRINTF("CLD\n");
    TRACE_AND_STEP();
    CLEAR_FLAG(F_DF);
    DECODE_CLEAR_SEGOVR();
    END_OF_INSTR();
}

/****************************************************************************
REMARKS:
Handles opcode 0xfd
****************************************************************************/
func x86emuOp_std(_ u8) {
    /* clear interrupts. */
    START_OF_INSTR();
    DECODE_PRINTF("STD\n");
    TRACE_AND_STEP();
    SET_FLAG(F_DF);
    DECODE_CLEAR_SEGOVR();
    END_OF_INSTR();
}

/****************************************************************************
REMARKS:
Handles opcode 0xfe
****************************************************************************/
func x86emuOp_opcFE_byte_RM(_ u8) {
    int mod, rh, rl;
    var destval u8
    var destoffset uint
    u8 *destreg;

    /* Yet another special case instruction. */
    START_OF_INSTR();
    FETCH_DECODE_MODRM(mod, rh, rl);

    if (DEBUG_DECODE()) {
        /* XXX DECODE_PRINTF may be changed to something more
           general, so that it is important to leave the strings
           in the same format, even though the result is that the
           above test is done twice. */

        switch (rh) {
        case 0:
            DECODE_PRINTF("INC\t");
            break;
        case 1:
            DECODE_PRINTF("DEC\t");
            break;
        case 2:
        case 3:
        case 4:
        case 5:
        case 6:
        case 7:
            DECODE_PRINTF2("ILLEGAL OP MAJOR OP 0xFE MINOR OP %x\n", mod);
            HALT_SYS();
            break;
        }
    }

    if (mod < 3) {
        DECODE_PRINTF("BYTE PTR ");
        destoffset = decode_rmXX_address(mod, rl);
        DECODE_PRINTF("\n");
        destval = fetch_data_byte(destoffset);
        TRACE_AND_STEP();
        if (rh == 0)
          destval = inc_byte(destval);
        else
          destval = dec_byte(destval);
        store_data_byte(destoffset, destval);
    } else {
        destreg = DECODE_RM_BYTE_REGISTER(rl);
        DECODE_PRINTF("\n");
        TRACE_AND_STEP();
        if (rh == 0)
          *destreg = inc_byte(*destreg);
        else
          *destreg = dec_byte(*destreg);
    }
    DECODE_CLEAR_SEGOVR();
    END_OF_INSTR();
}

/****************************************************************************
REMARKS:
Handles opcode 0xff
****************************************************************************/
func x86emuOp_opcFF_word_RM(_ u8) {
    int mod, rh, rl;
    uint destoffset = 0;
 var destreg *u16
 u32 *destreg32;
 u16 destval,destval2;
 u32 destval32;

    /* Yet another special case instruction. */
    START_OF_INSTR();
    FETCH_DECODE_MODRM(mod, rh, rl);

    if (DEBUG_DECODE()) {
        /* XXX DECODE_PRINTF may be changed to something more
           general, so that it is important to leave the strings
           in the same format, even though the result is that the
           above test is done twice. */

        switch (rh) {
        case 0:
            if (M.x86.mode & SYSMODE_PREFIX_DATA) {
                DECODE_PRINTF("INC\tDWORD PTR ");
            } else {
                DECODE_PRINTF("INC\tWORD PTR ");
            }
            break;
        case 1:
            if (M.x86.mode & SYSMODE_PREFIX_DATA) {
                DECODE_PRINTF("DEC\tDWORD PTR ");
            } else {
                DECODE_PRINTF("DEC\tWORD PTR ");
            }
            break;
        case 2:
            DECODE_PRINTF("CALL\t ");
            break;
        case 3:
            DECODE_PRINTF("CALL\tFAR ");
            break;
        case 4:
            DECODE_PRINTF("JMP\t");
            break;
        case 5:
            DECODE_PRINTF("JMP\tFAR ");
            break;
        case 6:
            DECODE_PRINTF("PUSH\t");
            break;
        case 7:
            DECODE_PRINTF("ILLEGAL DECODING OF OPCODE FF\t");
            HALT_SYS();
            break;
        }
    }

    if (mod < 3) {
        destoffset = decode_rmXX_address(mod, rl);
        DECODE_PRINTF("\n");
        switch (rh) {
        case 0: /* inc word ptr ... */
            if (M.x86.mode & SYSMODE_PREFIX_DATA) {
                destval32 = fetch_data_long(destoffset);
                TRACE_AND_STEP();
                destval32 = inc_long(destval32);
                store_data_long(destoffset, destval32);
            } else {
                destval = fetch_data_word(destoffset);
                TRACE_AND_STEP();
                destval = inc_word(destval);
                store_data_word(destoffset, destval);
            }
            break;
        case 1: /* dec word ptr ... */
            if (M.x86.mode & SYSMODE_PREFIX_DATA) {
                destval32 = fetch_data_long(destoffset);
                TRACE_AND_STEP();
                destval32 = dec_long(destval32);
                store_data_long(destoffset, destval32);
            } else {
                destval = fetch_data_word(destoffset);
                TRACE_AND_STEP();
                destval = dec_word(destval);
                store_data_word(destoffset, destval);
            }
            break;
        case 2: /* call word ptr ... */
            destval = fetch_data_word(destoffset);
            TRACE_AND_STEP();
            push_word(M.x86.R_IP);
            M.x86.R_IP = destval;
            break;
        case 3: /* call far ptr ... */
            destval = fetch_data_word(destoffset);
            destval2 = fetch_data_word(destoffset + 2);
            TRACE_AND_STEP();
            push_word(M.x86.R_CS);
            M.x86.R_CS = destval2;
            push_word(M.x86.R_IP);
            M.x86.R_IP = destval;
            break;
        case 4: /* jmp word ptr ... */
            destval = fetch_data_word(destoffset);
            JMP_TRACE(M.x86.saved_cs, M.x86.saved_ip, M.x86.R_CS, destval, " WORD ");
            TRACE_AND_STEP();
            M.x86.R_IP = destval;
            break;
        case 5: /* jmp far ptr ... */
            destval = fetch_data_word(destoffset);
            destval2 = fetch_data_word(destoffset + 2);
            JMP_TRACE(M.x86.saved_cs, M.x86.saved_ip, destval2, destval, " FAR ");
            TRACE_AND_STEP();
            M.x86.R_IP = destval;
            M.x86.R_CS = destval2;
            break;
        case 6: /*  push word ptr ... */
            if (M.x86.mode & SYSMODE_PREFIX_DATA) {
                destval32 = fetch_data_long(destoffset);
                TRACE_AND_STEP();
                push_long(destval32);
            } else {
                destval = fetch_data_word(destoffset);
                TRACE_AND_STEP();
                push_word(destval);
            }
            break;
        }
    } else {
        switch (rh) {
        case 0:
            if (M.x86.mode & SYSMODE_PREFIX_DATA) {
                destreg32 = DECODE_RM_LONG_REGISTER(rl);
                DECODE_PRINTF("\n");
                TRACE_AND_STEP();
                *destreg32 = inc_long(*destreg32);
            } else {
                destreg = DECODE_RM_WORD_REGISTER(rl);
                DECODE_PRINTF("\n");
                TRACE_AND_STEP();
                *destreg = inc_word(*destreg);
            }
            break;
        case 1:
            if (M.x86.mode & SYSMODE_PREFIX_DATA) {
                destreg32 = DECODE_RM_LONG_REGISTER(rl);
                DECODE_PRINTF("\n");
                TRACE_AND_STEP();
                *destreg32 = dec_long(*destreg32);
            } else {
                destreg = DECODE_RM_WORD_REGISTER(rl);
                DECODE_PRINTF("\n");
                TRACE_AND_STEP();
                *destreg = dec_word(*destreg);
            }
            break;
        case 2: /* call word ptr ... */
            destreg = DECODE_RM_WORD_REGISTER(rl);
            DECODE_PRINTF("\n");
            TRACE_AND_STEP();
            push_word(M.x86.R_IP);
            M.x86.R_IP = *destreg;
            break;
        case 3: /* jmp far ptr ... */
            DECODE_PRINTF("OPERATION UNDEFINED 0XFF\n");
            TRACE_AND_STEP();
            HALT_SYS();
            break;

        case 4: /* jmp  ... */
            destreg = DECODE_RM_WORD_REGISTER(rl);
            DECODE_PRINTF("\n");
            TRACE_AND_STEP();
            M.x86.R_IP = (u16) (*destreg);
            break;
        case 5: /* jmp far ptr ... */
            DECODE_PRINTF("OPERATION UNDEFINED 0XFF\n");
            TRACE_AND_STEP();
            HALT_SYS();
            break;
        case 6:
            if (M.x86.mode & SYSMODE_PREFIX_DATA) {
                destreg32 = DECODE_RM_LONG_REGISTER(rl);
                DECODE_PRINTF("\n");
                TRACE_AND_STEP();
                push_long(*destreg32);
            } else {
                destreg = DECODE_RM_WORD_REGISTER(rl);
                DECODE_PRINTF("\n");
                TRACE_AND_STEP();
                push_word(*destreg);
            }
            break;
        }
    }
    DECODE_CLEAR_SEGOVR();
    END_OF_INSTR();
}

/***************************************************************************
 * Single byte operation code table:
 **************************************************************************/
void (*x86emu_optab[256])(u8) =
{
/*  0x00 */ x86emuOp_genop_byte_RM_R,
/*  0x01 */ x86emuOp_genop_word_RM_R,
/*  0x02 */ x86emuOp_genop_byte_R_RM,
/*  0x03 */ x86emuOp_genop_word_R_RM,
/*  0x04 */ x86emuOp_genop_byte_AL_IMM,
/*  0x05 */ x86emuOp_genop_word_AX_IMM,
/*  0x06 */ x86emuOp_push_ES,
/*  0x07 */ x86emuOp_pop_ES,

/*  0x08 */ x86emuOp_genop_byte_RM_R,
/*  0x09 */ x86emuOp_genop_word_RM_R,
/*  0x0a */ x86emuOp_genop_byte_R_RM,
/*  0x0b */ x86emuOp_genop_word_R_RM,
/*  0x0c */ x86emuOp_genop_byte_AL_IMM,
/*  0x0d */ x86emuOp_genop_word_AX_IMM,
/*  0x0e */ x86emuOp_push_CS,
/*  0x0f */ x86emuOp_two_byte,

/*  0x10 */ x86emuOp_genop_byte_RM_R,
/*  0x11 */ x86emuOp_genop_word_RM_R,
/*  0x12 */ x86emuOp_genop_byte_R_RM,
/*  0x13 */ x86emuOp_genop_word_R_RM,
/*  0x14 */ x86emuOp_genop_byte_AL_IMM,
/*  0x15 */ x86emuOp_genop_word_AX_IMM,
/*  0x16 */ x86emuOp_push_SS,
/*  0x17 */ x86emuOp_pop_SS,

/*  0x18 */ x86emuOp_genop_byte_RM_R,
/*  0x19 */ x86emuOp_genop_word_RM_R,
/*  0x1a */ x86emuOp_genop_byte_R_RM,
/*  0x1b */ x86emuOp_genop_word_R_RM,
/*  0x1c */ x86emuOp_genop_byte_AL_IMM,
/*  0x1d */ x86emuOp_genop_word_AX_IMM,
/*  0x1e */ x86emuOp_push_DS,
/*  0x1f */ x86emuOp_pop_DS,

/*  0x20 */ x86emuOp_genop_byte_RM_R,
/*  0x21 */ x86emuOp_genop_word_RM_R,
/*  0x22 */ x86emuOp_genop_byte_R_RM,
/*  0x23 */ x86emuOp_genop_word_R_RM,
/*  0x24 */ x86emuOp_genop_byte_AL_IMM,
/*  0x25 */ x86emuOp_genop_word_AX_IMM,
/*  0x26 */ x86emuOp_segovr_ES,
/*  0x27 */ x86emuOp_daa,

/*  0x28 */ x86emuOp_genop_byte_RM_R,
/*  0x29 */ x86emuOp_genop_word_RM_R,
/*  0x2a */ x86emuOp_genop_byte_R_RM,
/*  0x2b */ x86emuOp_genop_word_R_RM,
/*  0x2c */ x86emuOp_genop_byte_AL_IMM,
/*  0x2d */ x86emuOp_genop_word_AX_IMM,
/*  0x2e */ x86emuOp_segovr_CS,
/*  0x2f */ x86emuOp_das,

/*  0x30 */ x86emuOp_genop_byte_RM_R,
/*  0x31 */ x86emuOp_genop_word_RM_R,
/*  0x32 */ x86emuOp_genop_byte_R_RM,
/*  0x33 */ x86emuOp_genop_word_R_RM,
/*  0x34 */ x86emuOp_genop_byte_AL_IMM,
/*  0x35 */ x86emuOp_genop_word_AX_IMM,
/*  0x36 */ x86emuOp_segovr_SS,
/*  0x37 */ x86emuOp_aaa,

/*  0x38 */ x86emuOp_genop_byte_RM_R,
/*  0x39 */ x86emuOp_genop_word_RM_R,
/*  0x3a */ x86emuOp_genop_byte_R_RM,
/*  0x3b */ x86emuOp_genop_word_R_RM,
/*  0x3c */ x86emuOp_genop_byte_AL_IMM,
/*  0x3d */ x86emuOp_genop_word_AX_IMM,
/*  0x3e */ x86emuOp_segovr_DS,
/*  0x3f */ x86emuOp_aas,

/*  0x40 */ x86emuOp_inc_register,
/*  0x41 */ x86emuOp_inc_register,
/*  0x42 */ x86emuOp_inc_register,
/*  0x43 */ x86emuOp_inc_register,
/*  0x44 */ x86emuOp_inc_register,
/*  0x45 */ x86emuOp_inc_register,
/*  0x46 */ x86emuOp_inc_register,
/*  0x47 */ x86emuOp_inc_register,

/*  0x48 */ x86emuOp_dec_register,
/*  0x49 */ x86emuOp_dec_register,
/*  0x4a */ x86emuOp_dec_register,
/*  0x4b */ x86emuOp_dec_register,
/*  0x4c */ x86emuOp_dec_register,
/*  0x4d */ x86emuOp_dec_register,
/*  0x4e */ x86emuOp_dec_register,
/*  0x4f */ x86emuOp_dec_register,

/*  0x50 */ x86emuOp_push_register,
/*  0x51 */ x86emuOp_push_register,
/*  0x52 */ x86emuOp_push_register,
/*  0x53 */ x86emuOp_push_register,
/*  0x54 */ x86emuOp_push_register,
/*  0x55 */ x86emuOp_push_register,
/*  0x56 */ x86emuOp_push_register,
/*  0x57 */ x86emuOp_push_register,

/*  0x58 */ x86emuOp_pop_register,
/*  0x59 */ x86emuOp_pop_register,
/*  0x5a */ x86emuOp_pop_register,
/*  0x5b */ x86emuOp_pop_register,
/*  0x5c */ x86emuOp_pop_register,
/*  0x5d */ x86emuOp_pop_register,
/*  0x5e */ x86emuOp_pop_register,
/*  0x5f */ x86emuOp_pop_register,

/*  0x60 */ x86emuOp_push_all,
/*  0x61 */ x86emuOp_pop_all,
/*  0x62 */ x86emuOp_illegal_op, /* bound */
/*  0x63 */ x86emuOp_illegal_op, /* arpl */
/*  0x64 */ x86emuOp_segovr_FS,
/*  0x65 */ x86emuOp_segovr_GS,
/*  0x66 */ x86emuOp_prefix_data,
/*  0x67 */ x86emuOp_prefix_addr,

/*  0x68 */ x86emuOp_push_word_IMM,
/*  0x69 */ x86emuOp_imul_word_IMM,
/*  0x6a */ x86emuOp_push_byte_IMM,
/*  0x6b */ x86emuOp_imul_byte_IMM,
/*  0x6c */ x86emuOp_ins_byte,
/*  0x6d */ x86emuOp_ins_word,
/*  0x6e */ x86emuOp_outs_byte,
/*  0x6f */ x86emuOp_outs_word,

/*  0x70 */ x86emuOp_jump_near_cond,
/*  0x71 */ x86emuOp_jump_near_cond,
/*  0x72 */ x86emuOp_jump_near_cond,
/*  0x73 */ x86emuOp_jump_near_cond,
/*  0x74 */ x86emuOp_jump_near_cond,
/*  0x75 */ x86emuOp_jump_near_cond,
/*  0x76 */ x86emuOp_jump_near_cond,
/*  0x77 */ x86emuOp_jump_near_cond,

/*  0x78 */ x86emuOp_jump_near_cond,
/*  0x79 */ x86emuOp_jump_near_cond,
/*  0x7a */ x86emuOp_jump_near_cond,
/*  0x7b */ x86emuOp_jump_near_cond,
/*  0x7c */ x86emuOp_jump_near_cond,
/*  0x7d */ x86emuOp_jump_near_cond,
/*  0x7e */ x86emuOp_jump_near_cond,
/*  0x7f */ x86emuOp_jump_near_cond,

/*  0x80 */ x86emuOp_opc80_byte_RM_IMM,
/*  0x81 */ x86emuOp_opc81_word_RM_IMM,
/*  0x82 */ x86emuOp_opc82_byte_RM_IMM,
/*  0x83 */ x86emuOp_opc83_word_RM_IMM,
/*  0x84 */ x86emuOp_test_byte_RM_R,
/*  0x85 */ x86emuOp_test_word_RM_R,
/*  0x86 */ x86emuOp_xchg_byte_RM_R,
/*  0x87 */ x86emuOp_xchg_word_RM_R,

/*  0x88 */ x86emuOp_mov_byte_RM_R,
/*  0x89 */ x86emuOp_mov_word_RM_R,
/*  0x8a */ x86emuOp_mov_byte_R_RM,
/*  0x8b */ x86emuOp_mov_word_R_RM,
/*  0x8c */ x86emuOp_mov_word_RM_SR,
/*  0x8d */ x86emuOp_lea_word_R_M,
/*  0x8e */ x86emuOp_mov_word_SR_RM,
/*  0x8f */ x86emuOp_pop_RM,

/*  0x90 */ x86emuOp_nop,
/*  0x91 */ x86emuOp_xchg_word_AX_register,
/*  0x92 */ x86emuOp_xchg_word_AX_register,
/*  0x93 */ x86emuOp_xchg_word_AX_register,
/*  0x94 */ x86emuOp_xchg_word_AX_register,
/*  0x95 */ x86emuOp_xchg_word_AX_register,
/*  0x96 */ x86emuOp_xchg_word_AX_register,
/*  0x97 */ x86emuOp_xchg_word_AX_register,

/*  0x98 */ x86emuOp_cbw,
/*  0x99 */ x86emuOp_cwd,
/*  0x9a */ x86emuOp_call_far_IMM,
/*  0x9b */ x86emuOp_wait,
/*  0x9c */ x86emuOp_pushf_word,
/*  0x9d */ x86emuOp_popf_word,
/*  0x9e */ x86emuOp_sahf,
/*  0x9f */ x86emuOp_lahf,

/*  0xa0 */ x86emuOp_mov_AL_M_IMM,
/*  0xa1 */ x86emuOp_mov_AX_M_IMM,
/*  0xa2 */ x86emuOp_mov_M_AL_IMM,
/*  0xa3 */ x86emuOp_mov_M_AX_IMM,
/*  0xa4 */ x86emuOp_movs_byte,
/*  0xa5 */ x86emuOp_movs_word,
/*  0xa6 */ x86emuOp_cmps_byte,
/*  0xa7 */ x86emuOp_cmps_word,
/*  0xa8 */ x86emuOp_test_AL_IMM,
/*  0xa9 */ x86emuOp_test_AX_IMM,
/*  0xaa */ x86emuOp_stos_byte,
/*  0xab */ x86emuOp_stos_word,
/*  0xac */ x86emuOp_lods_byte,
/*  0xad */ x86emuOp_lods_word,
/*  0xac */ x86emuOp_scas_byte,
/*  0xad */ x86emuOp_scas_word,

/*  0xb0 */ x86emuOp_mov_byte_register_IMM,
/*  0xb1 */ x86emuOp_mov_byte_register_IMM,
/*  0xb2 */ x86emuOp_mov_byte_register_IMM,
/*  0xb3 */ x86emuOp_mov_byte_register_IMM,
/*  0xb4 */ x86emuOp_mov_byte_register_IMM,
/*  0xb5 */ x86emuOp_mov_byte_register_IMM,
/*  0xb6 */ x86emuOp_mov_byte_register_IMM,
/*  0xb7 */ x86emuOp_mov_byte_register_IMM,

/*  0xb8 */ x86emuOp_mov_word_register_IMM,
/*  0xb9 */ x86emuOp_mov_word_register_IMM,
/*  0xba */ x86emuOp_mov_word_register_IMM,
/*  0xbb */ x86emuOp_mov_word_register_IMM,
/*  0xbc */ x86emuOp_mov_word_register_IMM,
/*  0xbd */ x86emuOp_mov_word_register_IMM,
/*  0xbe */ x86emuOp_mov_word_register_IMM,
/*  0xbf */ x86emuOp_mov_word_register_IMM,

/*  0xc0 */ x86emuOp_opcC0_byte_RM_MEM,
/*  0xc1 */ x86emuOp_opcC1_word_RM_MEM,
/*  0xc2 */ x86emuOp_ret_near_IMM,
/*  0xc3 */ x86emuOp_ret_near,
/*  0xc4 */ x86emuOp_les_R_IMM,
/*  0xc5 */ x86emuOp_lds_R_IMM,
/*  0xc6 */ x86emuOp_mov_byte_RM_IMM,
/*  0xc7 */ x86emuOp_mov_word_RM_IMM,
/*  0xc8 */ x86emuOp_enter,
/*  0xc9 */ x86emuOp_leave,
/*  0xca */ x86emuOp_ret_far_IMM,
/*  0xcb */ x86emuOp_ret_far,
/*  0xcc */ x86emuOp_int3,
/*  0xcd */ x86emuOp_int_IMM,
/*  0xce */ x86emuOp_into,
/*  0xcf */ x86emuOp_iret,

/*  0xd0 */ x86emuOp_opcD0_byte_RM_1,
/*  0xd1 */ x86emuOp_opcD1_word_RM_1,
/*  0xd2 */ x86emuOp_opcD2_byte_RM_CL,
/*  0xd3 */ x86emuOp_opcD3_word_RM_CL,
/*  0xd4 */ x86emuOp_aam,
/*  0xd5 */ x86emuOp_aad,
/*  0xd6 */ x86emuOp_illegal_op, /* Undocumented SETALC instruction */
/*  0xd7 */ x86emuOp_xlat,
/*  0xd8 */ x86emuOp_esc_coprocess_d8,
/*  0xd9 */ x86emuOp_esc_coprocess_d9,
/*  0xda */ x86emuOp_esc_coprocess_da,
/*  0xdb */ x86emuOp_esc_coprocess_db,
/*  0xdc */ x86emuOp_esc_coprocess_dc,
/*  0xdd */ x86emuOp_esc_coprocess_dd,
/*  0xde */ x86emuOp_esc_coprocess_de,
/*  0xdf */ x86emuOp_esc_coprocess_df,

/*  0xe0 */ x86emuOp_loopne,
/*  0xe1 */ x86emuOp_loope,
/*  0xe2 */ x86emuOp_loop,
/*  0xe3 */ x86emuOp_jcxz,
/*  0xe4 */ x86emuOp_in_byte_AL_IMM,
/*  0xe5 */ x86emuOp_in_word_AX_IMM,
/*  0xe6 */ x86emuOp_out_byte_IMM_AL,
/*  0xe7 */ x86emuOp_out_word_IMM_AX,

/*  0xe8 */ x86emuOp_call_near_IMM,
/*  0xe9 */ x86emuOp_jump_near_IMM,
/*  0xea */ x86emuOp_jump_far_IMM,
/*  0xeb */ x86emuOp_jump_byte_IMM,
/*  0xec */ x86emuOp_in_byte_AL_DX,
/*  0xed */ x86emuOp_in_word_AX_DX,
/*  0xee */ x86emuOp_out_byte_DX_AL,
/*  0xef */ x86emuOp_out_word_DX_AX,

/*  0xf0 */ x86emuOp_lock,
/*  0xf1 */ x86emuOp_illegal_op,
/*  0xf2 */ x86emuOp_repne,
/*  0xf3 */ x86emuOp_repe,
/*  0xf4 */ x86emuOp_halt,
/*  0xf5 */ x86emuOp_cmc,
/*  0xf6 */ x86emuOp_opcF6_byte_RM,
/*  0xf7 */ x86emuOp_opcF7_word_RM,

/*  0xf8 */ x86emuOp_clc,
/*  0xf9 */ x86emuOp_stc,
/*  0xfa */ x86emuOp_cli,
/*  0xfb */ x86emuOp_sti,
/*  0xfc */ x86emuOp_cld,
/*  0xfd */ x86emuOp_std,
/*  0xfe */ x86emuOp_opcFE_byte_RM,
/*  0xff */ x86emuOp_opcFF_word_RM,
};