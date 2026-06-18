package nativeopus

// Port of libopus/silk/pitch_est_tables.c.

var silk_CB_lags_stage2_10_ms = [PE_MAX_NB_SUBFR >> 1][PE_NB_CBKS_STAGE2_10MS]opus_int8{
	{0, 1, 0},
	{0, 0, 1},
}

var silk_CB_lags_stage3_10_ms = [PE_MAX_NB_SUBFR >> 1][PE_NB_CBKS_STAGE3_10MS]opus_int8{
	{0, 0, 1, -1, 1, -1, 2, -2, 2, -2, 3, -3},
	{0, 1, 0, 1, -1, 2, -1, 2, -2, 3, -2, 3},
}

var silk_Lag_range_stage3_10_ms = [PE_MAX_NB_SUBFR >> 1][2]opus_int8{
	{-3, 7},
	{-2, 7},
}

var silk_CB_lags_stage2 = [PE_MAX_NB_SUBFR][PE_NB_CBKS_STAGE2_EXT]opus_int8{
	{0, 2, -1, -1, -1, 0, 0, 1, 1, 0, 1},
	{0, 1, 0, 0, 0, 0, 0, 1, 0, 0, 0},
	{0, 0, 1, 0, 0, 0, 1, 0, 0, 0, 0},
	{0, -1, 2, 1, 0, 1, 1, 0, 0, -1, -1},
}

var silk_CB_lags_stage3 = [PE_MAX_NB_SUBFR][PE_NB_CBKS_STAGE3_MAX]opus_int8{
	{0, 0, 1, -1, 0, 1, -1, 0, -1, 1, -2, 2, -2, -2, 2, -3, 2, 3, -3, -4, 3, -4, 4, 4, -5, 5, -6, -5, 6, -7, 6, 5, 8, -9},
	{0, 0, 1, 0, 0, 0, 0, 0, 0, 0, -1, 1, 0, 0, 1, -1, 0, 1, -1, -1, 1, -1, 2, 1, -1, 2, -2, -2, 2, -2, 2, 2, 3, -3},
	{0, 1, 0, 0, 0, 0, 0, 0, 1, 0, 1, 0, 0, 1, -1, 1, 0, 0, 2, 1, -1, 2, -1, -1, 2, -1, 2, 2, -1, 3, -2, -2, -2, 3},
	{0, 1, 0, 0, 1, 0, 1, -1, 2, -1, 2, -1, 2, 3, -2, 3, -2, -2, 4, 4, -3, 5, -3, -4, 6, -4, 6, 5, -5, 8, -6, -5, -7, 9},
}

var silk_Lag_range_stage3 = [SILK_PE_MAX_COMPLEX + 1][PE_MAX_NB_SUBFR][2]opus_int8{
	{
		{-5, 8},
		{-1, 6},
		{-1, 6},
		{-4, 10},
	},
	{
		{-6, 10},
		{-2, 6},
		{-1, 6},
		{-5, 10},
	},
	{
		{-9, 12},
		{-3, 7},
		{-2, 7},
		{-7, 13},
	},
}

var silk_nb_cbk_searchs_stage3 = [SILK_PE_MAX_COMPLEX + 1]opus_int8{
	PE_NB_CBKS_STAGE3_MIN,
	PE_NB_CBKS_STAGE3_MID,
	PE_NB_CBKS_STAGE3_MAX,
}
