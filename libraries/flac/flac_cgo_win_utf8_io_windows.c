/* Windows-only amalgamation translation unit for libFLAC's UTF-8 I/O
 * shim. On _WIN32, share/compat.h maps flac_fopen/flac_rename/flac_stat/
 * … to fopen_utf8/rename_utf8/stat64_utf8/… which are defined here; the
 * compiled libFLAC sources (metadata_iterators.c, stream_encoder.c, …)
 * call those macros, so the implementation must be linked in on Windows.
 *
 * The _windows.c filename suffix makes cgo compile this TU only for
 * GOOS=windows, so non-Windows builds are unaffected (upstream's
 * win_utf8_io.c is not internally _WIN32-gated — it includes <io.h> and
 * <windows.h> unconditionally — hence the file-suffix gate rather than a
 * preprocessor guard).
 */

#include "libflac/src/share/win_utf8_io/win_utf8_io.c"
