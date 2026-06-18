/* Portable config.h for inlined libogg build via Go/Cgo.
 *
 * Provides standard header availability and size information
 * for all platforms Go supports.
 */

#define HAVE_STDINT_H 1
#define HAVE_INTTYPES_H 1
#define HAVE_STDIO_H 1
#define HAVE_STDLIB_H 1
#define HAVE_STRING_H 1
#define HAVE_STRINGS_H 1

#if defined(__unix__) || defined(__APPLE__)
#define HAVE_DLFCN_H 1
#define HAVE_UNISTD_H 1
#define HAVE_SYS_TYPES_H 1
#define HAVE_SYS_STAT_H 1
#endif

#define SIZEOF_SHORT 2
#define SIZEOF_INT 4
#define SIZEOF_LONG_LONG 8
#define SIZEOF_INT16_T 2
#define SIZEOF_UINT16_T 2
#define SIZEOF_INT32_T 4
#define SIZEOF_UINT32_T 4
#define SIZEOF_INT64_T 8
#define SIZEOF_UINT64_T 8

#if defined(__LP64__) || defined(_WIN64)
#define SIZEOF_LONG 8
#else
#define SIZEOF_LONG 4
#endif
