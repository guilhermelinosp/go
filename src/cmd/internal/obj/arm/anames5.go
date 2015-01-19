package arm

var anames5 = []string{
	"XXX",
	"AND",
	"EOR",
	"SUB",
	"RSB",
	"ADD",
	"ADC",
	"SBC",
	"RSC",
	"TST",
	"TEQ",
	"CMP",
	"CMN",
	"ORR",
	"BIC",
	"MVN",
	"B",
	"BL",
	"BEQ",
	"BNE",
	"BCS",
	"BHS",
	"BCC",
	"BLO",
	"BMI",
	"BPL",
	"BVS",
	"BVC",
	"BHI",
	"BLS",
	"BGE",
	"BLT",
	"BGT",
	"BLE",
	"MOVWD",
	"MOVWF",
	"MOVDW",
	"MOVFW",
	"MOVFD",
	"MOVDF",
	"MOVF",
	"MOVD",
	"CMPF",
	"CMPD",
	"ADDF",
	"ADDD",
	"SUBF",
	"SUBD",
	"MULF",
	"MULD",
	"DIVF",
	"DIVD",
	"SQRTF",
	"SQRTD",
	"ABSF",
	"ABSD",
	"SRL",
	"SRA",
	"SLL",
	"MULU",
	"DIVU",
	"MUL",
	"DIV",
	"MOD",
	"MODU",
	"MOVB",
	"MOVBS",
	"MOVBU",
	"MOVH",
	"MOVHS",
	"MOVHU",
	"MOVW",
	"MOVM",
	"SWPBU",
	"SWPW",
	"NOP",
	"RFE",
	"SWI",
	"MULA",
	"DATA",
	"GLOBL",
	"GOK",
	"HISTORY",
	"NAME",
	"RET",
	"TEXT",
	"WORD",
	"DYNT_",
	"INIT_",
	"BCASE",
	"CASE",
	"END",
	"MULL",
	"MULAL",
	"MULLU",
	"MULALU",
	"BX",
	"BXRET",
	"DWORD",
	"SIGNAME",
	"LDREX",
	"STREX",
	"LDREXD",
	"STREXD",
	"PLD",
	"UNDEF",
	"CLZ",
	"MULWT",
	"MULWB",
	"MULAWT",
	"MULAWB",
	"USEFIELD",
	"TYPE",
	"FUNCDATA",
	"PCDATA",
	"CHECKNIL",
	"VARDEF",
	"VARKILL",
	"DUFFCOPY",
	"DUFFZERO",
	"DATABUNDLE",
	"DATABUNDLEEND",
	"MRC",
	"LAST",
}

var cnames5 = []string{
	"NONE",
	"REG",
	"REGREG",
	"REGREG2",
	"SHIFT",
	"FREG",
	"PSR",
	"FCR",
	"RCON",
	"NCON",
	"SCON",
	"LCON",
	"LCONADDR",
	"ZFCON",
	"SFCON",
	"LFCON",
	"RACON",
	"LACON",
	"SBRA",
	"LBRA",
	"HAUTO",
	"FAUTO",
	"HFAUTO",
	"SAUTO",
	"LAUTO",
	"HOREG",
	"FOREG",
	"HFOREG",
	"SOREG",
	"ROREG",
	"SROREG",
	"LOREG",
	"PC",
	"SP",
	"HREG",
	"ADDR",
	"GOK",
	"NCLASS",
	"SCOND = (1<<4)-1",
	"SBIT = 1<<4",
	"PBIT = 1<<5",
	"WBIT = 1<<6",
	"FBIT = 1<<7",
	"UBIT = 1<<7",
	"SCOND_EQ = 0",
	"SCOND_NE = 1",
	"SCOND_HS = 2",
	"SCOND_LO = 3",
	"SCOND_MI = 4",
	"SCOND_PL = 5",
	"SCOND_VS = 6",
	"SCOND_VC = 7",
	"SCOND_HI = 8",
	"SCOND_LS = 9",
	"SCOND_GE = 10",
	"SCOND_LT = 11",
	"SCOND_GT = 12",
	"SCOND_LE = 13",
	"SCOND_NONE = 14",
	"SCOND_NV = 15",
}

var dnames5 = []string{
	D_GOK:  "GOK",
	D_NONE: "NONE",
}
