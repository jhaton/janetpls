//go:build cgo && libjanet

package main

/*
#cgo pkg-config: janet
#include <janet.h>
#include <stdint.h>
#include <stdlib.h>
#include <string.h>

static char *janet_lsp_copy_bytes(const uint8_t *bytes, int32_t length) {
    char *copy = malloc((size_t) length + 1);
    if (copy == NULL) return NULL;
    memcpy(copy, bytes, (size_t) length);
    copy[length] = '\0';
    return copy;
}

static int janet_lsp_compile(
    const uint8_t *source,
    int32_t length,
    const char *path,
    int32_t *line,
    int32_t *column,
    char **message
) {
    if (janet_init()) {
        static const char initialization_error[] = "libjanet initialization failed";
        *message = janet_lsp_copy_bytes((const uint8_t *) initialization_error, sizeof(initialization_error) - 1);
        return -1;
    }

    JanetParser parser;
    janet_parser_init(&parser);
    for (int32_t index = 0; index < length; index++) {
        janet_parser_consume(&parser, source[index]);
        if (janet_parser_status(&parser) == JANET_PARSE_ERROR) break;
    }
    if (janet_parser_status(&parser) != JANET_PARSE_ERROR) {
        janet_parser_eof(&parser);
    }
    if (janet_parser_status(&parser) == JANET_PARSE_ERROR) {
        *line = (int32_t) parser.line;
        *column = (int32_t) parser.column;
        const char *error = janet_parser_error(&parser);
        if (error == NULL) error = "libjanet parse error";
        *message = janet_lsp_copy_bytes((const uint8_t *) error, (int32_t) strlen(error));
        janet_parser_deinit(&parser);
        janet_deinit();
        return *message == NULL ? -1 : 1;
    }

    JanetTable *environment = janet_core_env(NULL);
    janet_gcroot(janet_wrap_table(environment));
    JanetArray *forms = janet_array(8);
    janet_gcroot(janet_wrap_array(forms));
    janet_array_push(forms, janet_csymbolv("do"));
    while (janet_parser_has_more(&parser)) {
        janet_array_push(forms, janet_parser_produce(&parser));
    }
    Janet source_form = janet_wrap_tuple(janet_tuple_n(forms->data, forms->count));
    janet_gcroot(source_form);
    Janet where = janet_wrap_string(janet_cstring(path));
    janet_gcroot(where);
    JanetCompileResult result = janet_compile(source_form, environment, janet_unwrap_string(where));
    int status = 0;
    if (result.status == JANET_COMPILE_ERROR) {
        *line = result.error_mapping.line;
        *column = result.error_mapping.column;
        *message = janet_lsp_copy_bytes(result.error, janet_string_length(result.error));
        status = *message == NULL ? -1 : 1;
    }

    janet_gcunroot(where);
    janet_gcunroot(source_form);
    janet_gcunroot(janet_wrap_array(forms));
    janet_gcunroot(janet_wrap_table(environment));
    janet_parser_deinit(&parser);
    janet_deinit();
    return status;
}
*/
import "C"

import (
	"errors"
	"math"
	"unsafe"

	"github.com/jhaton/janet-lsp/internal/compiler"
)

func compileSource(request compiler.Request) (compiler.Response, error) {
	if len(request.Source) > math.MaxInt32 {
		return compiler.Response{}, errors.New("source exceeds libjanet input limit")
	}
	source := C.CBytes([]byte(request.Source))
	defer C.free(source)
	path := C.CString(request.Path)
	defer C.free(unsafe.Pointer(path))
	var line C.int32_t
	var column C.int32_t
	var message *C.char
	status := C.janet_lsp_compile(
		(*C.uint8_t)(source),
		C.int32_t(len(request.Source)),
		path,
		&line,
		&column,
		&message,
	)
	if message != nil {
		defer C.free(unsafe.Pointer(message))
	}
	if status < 0 {
		if message == nil {
			return compiler.Response{}, errors.New("libjanet compilation failed")
		}
		return compiler.Response{}, errors.New(C.GoString(message))
	}
	response := compiler.Response{Diagnostics: []compiler.Diagnostic{}}
	if status > 0 {
		response.Diagnostics = append(response.Diagnostics, compiler.Diagnostic{
			Line:    max(int(line)-1, 0),
			Column:  max(int(column)-1, 0),
			Message: C.GoString(message),
		})
	}
	return response, nil
}
