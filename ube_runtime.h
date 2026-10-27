#ifndef UBE_RUNTIME_H
#define UBE_RUNTIME_H

#define _GNU_SOURCE
#include <stdio.h>
#include <stdlib.h>
#include <stdint.h>
#include <stdbool.h>
#include <string.h>
#include <math.h>
#include <pthread.h>
#include <errno.h>
#include <time.h>

#define UBE_TUBER_PAGE_SIZE (64 * 1024)

// --- 1. Thread-Local Isolated Tuber Memory ---
typedef struct {
    uint8_t* buffer;
    size_t top;
    size_t capacity;
} UbeTuber;

#if defined(__STDC_VERSION__) && __STDC_VERSION__ >= 201112L
static _Thread_local UbeTuber current_tuber;
#elif defined(__GNUC__) || defined(__clang__)
static __thread UbeTuber current_tuber;
#else
static UbeTuber current_tuber;
#endif

static inline void ube_init(void) {
    if (current_tuber.buffer == NULL) {
        current_tuber.capacity = UBE_TUBER_PAGE_SIZE;
        current_tuber.buffer = (uint8_t*)malloc(current_tuber.capacity);
        current_tuber.top = 0;
    }
}

static inline size_t ube_tuber_save(void) {
    return current_tuber.top;
}

static inline void ube_tuber_restore(size_t watermark) {
    current_tuber.top = watermark;
}

static inline void* ube_bump_alloc_unaligned(size_t size) {
    if (current_tuber.top + size > current_tuber.capacity) {
        size_t new_cap = current_tuber.capacity * 2;
        if (new_cap < current_tuber.top + size) new_cap = current_tuber.top + size + UBE_TUBER_PAGE_SIZE;
        uint8_t* new_buf = (uint8_t*)realloc(current_tuber.buffer, new_cap);
        if (!new_buf) {
            fprintf(stderr, "[Ube Panic]: Fatal out of memory on Tuber expansion!\n");
            exit(1);
        }
        current_tuber.buffer = new_buf;
        current_tuber.capacity = new_cap;
    }
    void* ptr = &current_tuber.buffer[current_tuber.top];
    current_tuber.top += size;
    return ptr;
}

static inline void* ube_bump_alloc(size_t size) {
    size = (size + 7) & ~7; // 8-byte alignment
    return ube_bump_alloc_unaligned(size);
}

// --- 2. String Type ---
typedef struct {
    const char* data;
    size_t len;
} string;

static inline string ube_str_lit(const char* lit) {
    string s = { lit, lit ? strlen(lit) : 0 };
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

// --- 3. Generational Slot Pool (Pool[T]) ---
typedef struct {
    uint32_t index;
    uint32_t gen;
} UbeHandle;

#define DEFINE_UBE_POOL(T) \
typedef struct { \
    T data; \
    uint32_t gen; \
    bool active; \
} UbeSlot_##T; \
typedef struct { \
    UbeSlot_##T* slots; \
    uint32_t* freelist; \
    size_t capacity; \
    size_t count; \
    size_t free_count; \
} Pool_##T; \
static inline Pool_##T* Pool_##T##_new(size_t capacity) { \
    Pool_##T* p = (Pool_##T*)malloc(sizeof(Pool_##T)); \
    p->capacity = capacity; \
    p->count = 0; \
    p->free_count = 0; \
    p->slots = (UbeSlot_##T*)calloc(capacity, sizeof(UbeSlot_##T)); \
    p->freelist = (uint32_t*)malloc(capacity * sizeof(uint32_t)); \
    return p; \
} \
static inline UbeHandle Pool_##T##_spawn(Pool_##T* p, T val) { \
    uint32_t idx; \
    if (p->free_count > 0) { \
        idx = p->freelist[--p->free_count]; \
    } else { \
        if (p->count >= p->capacity) { \
            p->capacity *= 2; \
            p->slots = (UbeSlot_##T*)realloc(p->slots, p->capacity * sizeof(UbeSlot_##T)); \
            p->freelist = (uint32_t*)realloc(p->freelist, p->capacity * sizeof(uint32_t)); \
        } \
        idx = (uint32_t)(p->count++); \
        p->slots[idx].gen = 1; \
    } \
    p->slots[idx].data = val; \
    p->slots[idx].active = true; \
    UbeHandle h = { idx, p->slots[idx].gen }; \
    return h; \
} \
static inline T* Pool_##T##_get(Pool_##T* p, UbeHandle h) { \
    if (h.index >= p->count || !p->slots[h.index].active || p->slots[h.index].gen != h.gen) return NULL; \
    return &p->slots[h.index].data; \
} \
static inline bool Pool_##T##_despawn(Pool_##T* p, UbeHandle h) { \
    if (h.index >= p->count || !p->slots[h.index].active || p->slots[h.index].gen != h.gen) return false; \
    p->slots[h.index].active = false; \
    p->slots[h.index].gen++; \
    p->freelist[p->free_count++] = h.index; \
    return true; \
}

// --- 4. Thread-Safe Ring Buffer Channels with Non-Blocking Try & Select ---
typedef struct {
    void* buffer;
    size_t elem_size;
    size_t capacity;
    size_t count;
    size_t head;
    size_t tail;
    bool closed;
    pthread_mutex_t lock;
    pthread_cond_t not_empty;
    pthread_cond_t not_full;
} UbeRawChan;

static inline UbeRawChan* ube_raw_chan_new(size_t capacity, size_t elem_size) {
    UbeRawChan* ch = (UbeRawChan*)malloc(sizeof(UbeRawChan));
    ch->buffer = malloc(capacity * elem_size);
    ch->elem_size = elem_size;
    ch->capacity = capacity;
    ch->count = 0;
    ch->head = 0;
    ch->tail = 0;
    ch->closed = false;
    pthread_mutex_init(&ch->lock, NULL);
    pthread_cond_init(&ch->not_empty, NULL);
    pthread_cond_init(&ch->not_full, NULL);
    return ch;
}

static inline void ube_raw_chan_send(UbeRawChan* ch, const void* val) {
    pthread_mutex_lock(&ch->lock);
    while (ch->count == ch->capacity && !ch->closed) {
        pthread_cond_wait(&ch->not_full, &ch->lock);
    }
    if (ch->closed) {
        pthread_mutex_unlock(&ch->lock);
        return;
    }
    memcpy((uint8_t*)ch->buffer + (ch->tail * ch->elem_size), val, ch->elem_size);
    ch->tail = (ch->tail + 1) % ch->capacity;
    ch->count++;
    pthread_cond_signal(&ch->not_empty);
    pthread_mutex_unlock(&ch->lock);
}

static inline bool ube_raw_chan_recv(UbeRawChan* ch, void* out) {
    pthread_mutex_lock(&ch->lock);
    while (ch->count == 0 && !ch->closed) {
        pthread_cond_wait(&ch->not_empty, &ch->lock);
    }
    if (ch->count == 0 && ch->closed) {
        pthread_mutex_unlock(&ch->lock);
        return false;
    }
    memcpy(out, (uint8_t*)ch->buffer + (ch->head * ch->elem_size), ch->elem_size);
    ch->head = (ch->head + 1) % ch->capacity;
    ch->count--;
    pthread_cond_signal(&ch->not_full);
    pthread_mutex_unlock(&ch->lock);
    return true;
}

static inline bool ube_raw_chan_try_recv(UbeRawChan* ch, void* out) {
    pthread_mutex_lock(&ch->lock);
    if (ch->count == 0) {
        pthread_mutex_unlock(&ch->lock);
        return false;
    }
    memcpy(out, (uint8_t*)ch->buffer + (ch->head * ch->elem_size), ch->elem_size);
    ch->head = (ch->head + 1) % ch->capacity;
    ch->count--;
    pthread_cond_signal(&ch->not_full);
    pthread_mutex_unlock(&ch->lock);
    return true;
}

static inline void ube_raw_chan_close(UbeRawChan* ch) {
    pthread_mutex_lock(&ch->lock);
    ch->closed = true;
    pthread_cond_broadcast(&ch->not_empty);
    pthread_cond_broadcast(&ch->not_full);
    pthread_mutex_unlock(&ch->lock);
}

// --- 5. Thread Trampoline ---
typedef struct {
    void* (*fn)(void*);
    void* arg;
} UbeThreadArgs;

static void* ube_thread_trampoline(void* raw) {
    UbeThreadArgs* a = (UbeThreadArgs*)raw;
    ube_init();
    size_t watermark = ube_tuber_save();
    a->fn(a->arg);
    ube_tuber_restore(watermark);
    free(a);
    return NULL;
}

static inline void ube_spawn(void* (*fn)(void*), void* arg) {
    pthread_t th;
    UbeThreadArgs* a = (UbeThreadArgs*)malloc(sizeof(UbeThreadArgs));
    a->fn = fn;
    a->arg = arg;
    pthread_create(&th, NULL, ube_thread_trampoline, a);
    pthread_detach(th);
}

// --- 6. Built-in Standard Library Wrappers ---
static inline void ube_fmt_println_str(const char* s) { printf("%s\n", s); }
static inline void println_str(const char* s) { printf("%s\n", s); }
static inline void println(int32_t v) { printf("%d\n", v); }
static inline void ube_fmt_println_i32(int32_t v) { printf("%d\n", v); }
static inline void ube_fmt_println_f32(float v) { printf("%f\n", v); }
static inline void ube_fmt_println_bool(bool v) { printf("%s\n", v ? "true" : "false"); }

static inline bool ube_fs_exists(const char* path) {
    FILE* f = fopen(path, "r");
    if (f) { fclose(f); return true; }
    return false;
}

static inline string ube_fs_read_file(const char* path) {
    FILE* f = fopen(path, "rb");
    if (!f) return ube_str_lit("");
    fseek(f, 0, SEEK_END);
    long sz = ftell(f);
    fseek(f, 0, SEEK_SET);
    char* buf = (char*)ube_bump_alloc(sz + 1);
    fread(buf, 1, sz, f);
    buf[sz] = '\0';
    fclose(f);
    string s = { buf, (size_t)sz };
    return s;
}

#endif // UBE_RUNTIME_H
