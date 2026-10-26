#ifndef UBE_RUNTIME_H
#define UBE_RUNTIME_H

#include <stdio.h>
#include <stdlib.h>
#include <stdint.h>
#include <stdbool.h>
#include <string.h>
#include <pthread.h>

#define UBE_TUBER_PAGE_SIZE (64 * 1024)

// Every Vine owns its own Tuber. `ube_init` set up the main Vine's Tuber;
// a spawned Vine's trampoline calls `ube_thread_init` before it runs.
#if defined(_MSC_VER)
#define UBE_TLS __declspec(thread)
#else
#define UBE_TLS __thread
#endif

typedef struct {
    uint8_t* buffer;
    size_t top;
    size_t capacity;
} UbeTuber;

static UBE_TLS UbeTuber global_tuber;

static inline void ube_thread_init(void) {
    global_tuber.capacity = UBE_TUBER_PAGE_SIZE;
    global_tuber.buffer = (uint8_t*)malloc(global_tuber.capacity);
    if (!global_tuber.buffer) {
        fprintf(stderr, "[Ube Panic]: cannot reserve Tuber!\n");
        exit(1);
    }
    global_tuber.top = 0;
}

static inline void ube_thread_shutdown(void) {
    free(global_tuber.buffer);
    global_tuber.buffer = NULL;
    global_tuber.top = 0;
    global_tuber.capacity = 0;
}

static inline void ube_init(void) {
    ube_thread_init();
}

static inline size_t ube_tuber_save(void) {
    return global_tuber.top;
}

static inline void ube_tuber_restore(size_t watermark) {
    global_tuber.top = watermark;
}

static inline void* ube_bump_alloc(size_t size) {
    size = (size + 7) & ~(size_t)7; // 8-byte alignment
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

// --- Optionals: T? (no null pointers, no GC) ---
//
// An optional is a tagged value: `has_val` plus the value itself. `null`
// becomes `has_val = false`; `if let x = opt { ... }` checks the tag before
// reading the value, so there is no way to dereference a null.
#define UBE_DEFINE_OPTION(suffix, ctype_)                                  \
    typedef struct {                                                       \
        bool has_val;                                                      \
        ctype_ val;                                                        \
    } UbeOption_##suffix;                                                  \
    static inline UbeOption_##suffix ube_some_##suffix(ctype_ v) {         \
        UbeOption_##suffix o;                                              \
        o.has_val = true;                                                  \
        o.val = v;                                                         \
        return o;                                                          \
    }                                                                      \
    static inline UbeOption_##suffix ube_none_##suffix(void) {             \
        UbeOption_##suffix o;                                              \
        o.has_val = false;                                                 \
        o.val = (ctype_){ 0 };                                             \
        return o;                                                          \
    }

UBE_DEFINE_OPTION(i32, int32_t)
UBE_DEFINE_OPTION(i64, int64_t)
UBE_DEFINE_OPTION(u32, uint32_t)
UBE_DEFINE_OPTION(u64, uint64_t)
UBE_DEFINE_OPTION(u8, uint8_t)
UBE_DEFINE_OPTION(f32, float)
UBE_DEFINE_OPTION(f64, double)
UBE_DEFINE_OPTION(bool, bool)
UBE_DEFINE_OPTION(string, string)

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

// ---------------------------------------------------------------------------
// Vines and channels (shared-nothing concurrency)
//
// A channel is a runtime-owned ring buffer; it is *not* on a Tuber because it
// outlives the Vine that created it. Values cross a channel only by memcpy of
// a flat value, so no pointer can escape into another Vine's Tuber.
// ---------------------------------------------------------------------------

typedef struct UbeChanImpl* UbeChan;

struct UbeChanImpl {
    uint8_t* buffer;
    size_t elem_size;
    size_t capacity;
    size_t head;   // next write slot
    size_t tail;   // next read slot
    size_t count;  // occupied slots
    bool closed;
    pthread_mutex_t lock;
    pthread_cond_t not_empty;
    pthread_cond_t not_full;
};

static inline UbeChan ube_chan_new(size_t elem_size, size_t capacity) {
    if (capacity == 0) capacity = 1;
    UbeChan ch = (UbeChan)calloc(1, sizeof(struct UbeChanImpl));
    if (!ch) {
        fprintf(stderr, "[Ube Panic]: out of memory creating channel!\n");
        exit(1);
    }
    ch->buffer = (uint8_t*)malloc(elem_size * capacity);
    if (!ch->buffer) {
        fprintf(stderr, "[Ube Panic]: out of memory creating channel!\n");
        exit(1);
    }
    ch->elem_size = elem_size;
    ch->capacity = capacity;
    ch->head = 0;
    ch->tail = 0;
    ch->count = 0;
    ch->closed = false;
    pthread_mutex_init(&ch->lock, NULL);
    pthread_cond_init(&ch->not_empty, NULL);
    pthread_cond_init(&ch->not_full, NULL);
    return ch;
}

static inline bool ube_chan_send(UbeChan ch, const void* val) {
    pthread_mutex_lock(&ch->lock);
    while (ch->count == ch->capacity && !ch->closed)
        pthread_cond_wait(&ch->not_full, &ch->lock);
    if (ch->closed) {
        pthread_mutex_unlock(&ch->lock);
        return false;
    }
    memcpy(&ch->buffer[ch->head * ch->elem_size], val, ch->elem_size);
    ch->head = (ch->head + 1) % ch->capacity;
    ch->count++;
    pthread_cond_signal(&ch->not_empty);
    pthread_mutex_unlock(&ch->lock);
    return true;
}

static inline bool ube_chan_recv(UbeChan ch, void* out) {
    pthread_mutex_lock(&ch->lock);
    while (ch->count == 0 && !ch->closed)
        pthread_cond_wait(&ch->not_empty, &ch->lock);
    if (ch->count == 0) { // closed and drained
        pthread_mutex_unlock(&ch->lock);
        memset(out, 0, ch->elem_size);
        return false;
    }
    memcpy(out, &ch->buffer[ch->tail * ch->elem_size], ch->elem_size);
    ch->tail = (ch->tail + 1) % ch->capacity;
    ch->count--;
    pthread_cond_signal(&ch->not_full);
    pthread_mutex_unlock(&ch->lock);
    return true;
}

static inline void ube_chan_close(UbeChan ch) {
    pthread_mutex_lock(&ch->lock);
    ch->closed = true;
    pthread_cond_broadcast(&ch->not_empty);
    pthread_cond_broadcast(&ch->not_full);
    pthread_mutex_unlock(&ch->lock);
}

// A Vine runs on an OS thread (pthreads backend). The trampoline the
// compiler emits initialises the Vine's private Tuber and frees its argument.
typedef void* (*UbeSpawnFn)(void*);

static inline void ube_spawn(UbeSpawnFn fn, void* arg) {
    pthread_t t;
    if (pthread_create(&t, NULL, fn, arg) != 0) {
        fprintf(stderr, "[Ube Panic]: cannot spawn Vine!\n");
        exit(1);
    }
    pthread_detach(t);
}

#endif // UBE_RUNTIME_H
