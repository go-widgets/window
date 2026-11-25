// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

#include "textflag.h"

// cbCommon is where every entry of cbEntries lands, with R16 holding the slot
// and R0-R7 holding the arguments of the C call, untouched. It runs on the
// native caller's stack and returns to the native caller.
//
// It saves LR and D8-D15 — the registers the Windows ARM64 convention makes
// callee-saved and runtime·callbackasm1 does not preserve — calls the callback
// entry the slot names, and puts them back. R0 is the result and is left alone.
// R16 and R17 are the intra-procedure-call scratch registers, free to use.
TEXT ·cbCommon(SB),NOSPLIT|NOFRAME,$0-0
	SUB	$80, RSP, RSP
	MOVD	LR, 0(RSP)
	FSTPD	(F8, F9), 16(RSP)
	FSTPD	(F10, F11), 32(RSP)
	FSTPD	(F12, F13), 48(RSP)
	FSTPD	(F14, F15), 64(RSP)
	MOVD	$·table(SB), R17
	MOVD	(R17)(R16<<3), R16
	BL	(R16)
	FLDPD	16(RSP), (F8, F9)
	FLDPD	32(RSP), (F10, F11)
	FLDPD	48(RSP), (F12, F13)
	FLDPD	64(RSP), (F14, F15)
	MOVD	0(RSP), LR
	ADD	$80, RSP, RSP
	RET

TEXT ·cbEntriesAddr(SB),NOSPLIT,$0-8
	MOVD	$·cbEntries(SB), R0
	MOVD	R0, ret+0(FP)
	RET

// The rest is the test's: a C-convention callee that clobbers D8-D15, and a
// C-convention caller that holds known doubles in them across a call.

// clobber returns R0+R1 and overwrites D8-D15, as a Go callback may.
TEXT ·clobber(SB),NOSPLIT|NOFRAME,$0-0
	MOVD	$0xdead, R2
	FMOVD	R2, F8
	FMOVD	R2, F9
	FMOVD	R2, F10
	FMOVD	R2, F11
	FMOVD	R2, F12
	FMOVD	R2, F13
	FMOVD	R2, F14
	FMOVD	R2, F15
	ADD	R1, R0, R0
	RET

TEXT ·clobberAddr(SB),NOSPLIT,$0-8
	MOVD	$·clobber(SB), R0
	MOVD	R0, ret+0(FP)
	RET

// func probe(fn uintptr, out *[8]uint64, a, b uint64) uint64
// Loads D8-D15 with 0x7ff0abcd00000001.., calls fn(a, b) by the C convention,
// stores D8-D15 as they are afterwards into out, and returns fn's result.
TEXT ·probe(SB),NOSPLIT,$16-40
	MOVD	fn+0(FP), R16
	MOVD	out+8(FP), R19
	MOVD	$0x7ff0abcd00000001, R2
	FMOVD	R2, F8
	ADD	$1, R2, R2
	FMOVD	R2, F9
	ADD	$1, R2, R2
	FMOVD	R2, F10
	ADD	$1, R2, R2
	FMOVD	R2, F11
	ADD	$1, R2, R2
	FMOVD	R2, F12
	ADD	$1, R2, R2
	FMOVD	R2, F13
	ADD	$1, R2, R2
	FMOVD	R2, F14
	ADD	$1, R2, R2
	FMOVD	R2, F15
	MOVD	a+16(FP), R0
	MOVD	b+24(FP), R1
	BL	(R16)
	FSTPD	(F8, F9), 0(R19)
	FSTPD	(F10, F11), 16(R19)
	FSTPD	(F12, F13), 32(R19)
	FSTPD	(F14, F15), 48(R19)
	MOVD	R0, ret+32(FP)
	RET
