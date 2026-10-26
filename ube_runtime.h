#ifndef UBE_RUNTIME_H
#define UBE_RUNTIME_H

#include <stdio.h>
#include <stdlib.h>
#include <stdint.h>
#include <stdbool.h>

// 64 KB default Tuber page (Matches WASM page size!)
#define UBE_TUBER_PAGE_SIZE (64 * 1024)

typedef struct {
    uint8_t* buffer;
    size_t top;
    size_t capacity;
} UbeTuber;

// Global thread/task Tuber scratchpad
static UbeTuber global_tuber;

static inline void ube_init() {
    global_tuber.capacity = UBE_TUBER_PAGE_SIZE;
    global_tuber.buffer = (uint8_t*)malloc(global_tuber.capacity);
    global_tuber.top = 0;
}

// Scoped watermarking: Freeing memory is just restoring `top`
static inline size_t ube_tuber_save() {
    return global_tuber.top;
}

static inline void ube_tuber_restore(size_t watermark) {
    global_tuber.top = watermark;
}

// Bump-allocate raw bytes
static inline void* ube_bump_alloc(size_t size) {
    // 8-byte alignment
    size = (size + 7) & ~7;
    if (global_tuber.top + size > global_tuber.capacity) {
        fprintf(stderr, "[Ube Panic]: Tuber scratchpad out of memory!\n");
        exit(1);
    }
    void* ptr = &global_tuber.buffer[global_tuber.top];
    global_tuber.top += size;
    return ptr;
}

// --- Dynamic Slice for i32: []i32 ---
typedef struct {
    int32_t* data;
    size_t len;
} UbeSlice_i32;

static inline UbeSlice_i32 ube_slice_i32_new() {
    // Starts pointing to the current top of the Tuber
    UbeSlice_i32 s;
    s.data = (int32_t*)&global_tuber.buffer[global_tuber.top];
    s.len = 0;
    return s;
}

// Pushing is a simple pointer bump! References into it NEVER invalidate.
static inline void ube_slice_i32_push(UbeSlice_i32* s, int32_t val) {
    int32_t* slot = (int32_t*)ube_bump_alloc(sizeof(int32_t));
    *slot = val;
    s->len++;
}

#endif // UBE_RUNTIME_H
