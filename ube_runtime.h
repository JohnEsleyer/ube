#ifndef UBE_RUNTIME_H
#define UBE_RUNTIME_H

#include <stdio.h>
#include <stdlib.h>
#include <stdint.h>
#include <stdbool.h>
#include <string.h>

#define UBE_TUBER_PAGE_SIZE (64 * 1024)

typedef struct {
    uint8_t* buffer;
    size_t top;
    size_t capacity;
} UbeTuber;

static UbeTuber global_tuber;

static inline void ube_init() {
    global_tuber.capacity = UBE_TUBER_PAGE_SIZE;
    global_tuber.buffer = (uint8_t*)malloc(global_tuber.capacity);
    global_tuber.top = 0;
}

static inline size_t ube_tuber_save() {
    return global_tuber.top;
}

static inline void ube_tuber_restore(size_t watermark) {
    global_tuber.top = watermark;
}

static inline void* ube_bump_alloc(size_t size) {
    size = (size + 7) & ~7; // 8-byte alignment
    if (global_tuber.top + size > global_tuber.capacity) {
        fprintf(stderr, "[Ube Panic]: Tuber scratchpad out of memory!\n");
        exit(1);
    }
    void* ptr = &global_tuber.buffer[global_tuber.top];
    global_tuber.top += size;
    return ptr;
}

// --- Dynamic Slice for i32 ---
//
// Zero-GC returns: a function returning UbeSlice_i32 builds its elements on
// the Tuber and does NOT restore its watermark, so the elements stay above
// the caller's watermark. The caller's scope exit frees them in one store.
typedef struct {
    int32_t* data;
    size_t len;
} UbeSlice_i32;

static inline UbeSlice_i32 ube_slice_i32_new() {
    UbeSlice_i32 s;
    // A slice owns the top of the Tuber: its elements are written
    // contiguously from here, so align the *base* once.
    global_tuber.top = (global_tuber.top + 7) & ~(size_t)7;
    s.data = (int32_t*)&global_tuber.buffer[global_tuber.top];
    s.len = 0;
    return s;
}

static inline void ube_slice_i32_push(UbeSlice_i32* s, int32_t val) {
    // Elements are contiguous (4-byte stride). The slice is the growing
    // value on top of the Tuber, so nothing else may allocate here while
    // it grows. Growth is one pointer bump; nothing ever moves.
    if (global_tuber.top + sizeof(int32_t) > global_tuber.capacity) {
        fprintf(stderr, "[Ube Panic]: Tuber scratchpad out of memory!\n");
        exit(1);
    }
    s->data[s->len] = val;
    global_tuber.top += sizeof(int32_t);
    s->len++;
}

// --- Strings (Zero GC, lives on Tuber) ---
typedef struct {
    const char* data;
    size_t len;
} string;

static inline string ube_str_lit(const char* lit) {
    string s = { lit, strlen(lit) };
    return s;
}

static inline string ube_str_concat(string a, string b) {
    size_t total = a.len + b.len;
    char* dest = (char*)ube_bump_alloc(total + 1);
    memcpy(dest, a.data, a.len);
    memcpy(dest + a.len, b.data, b.len);
    dest[total] = '\0';
    string s = { dest, total };
    return s;
}

// --- Result Type: i32 ! string ---
typedef struct {
    bool ok;
    union {
        int32_t val;
        string err;
    };
} UbeResult_i32;

static inline UbeResult_i32 ube_ok_i32(int32_t val) {
    UbeResult_i32 r;
    r.ok = true;
    r.val = val;
    return r;
}

static inline UbeResult_i32 ube_err_i32(string err) {
    UbeResult_i32 r;
    r.ok = false;
    r.err = err;
    return r;
}

#endif // UBE_RUNTIME_H
