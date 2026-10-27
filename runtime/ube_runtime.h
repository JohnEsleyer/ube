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

#define UBE_SCOPE_CHUNK_SIZE (64 * 1024)

// --- 1. Chunked Immutable Scopes (Arena Allocator) ---
typedef struct UbeChunk {
    struct UbeChunk* next;
    size_t capacity;
    uint8_t memory[];
} UbeChunk;

typedef struct {
    UbeChunk* head;
    UbeChunk* current;
    size_t top;          // Offset in current chunk
    size_t total_chunks; // Number of chunks in use
} UbeScopeArena;

typedef struct {
    UbeChunk* chunk;
    size_t top;
} UbeWatermark;

#if defined(__STDC_VERSION__) && __STDC_VERSION__ >= 201112L
static _Thread_local UbeScopeArena current_scope;
#elif defined(__GNUC__) || defined(__clang__)
static __thread UbeScopeArena current_scope;
#else
static UbeScopeArena current_scope;
#endif

static inline UbeChunk* ube_chunk_alloc(size_t min_capacity) {
    size_t cap = min_capacity > UBE_SCOPE_CHUNK_SIZE ? min_capacity : UBE_SCOPE_CHUNK_SIZE;
    UbeChunk* c = (UbeChunk*)malloc(sizeof(UbeChunk) + cap);
    if (!c) {
        fprintf(stderr, "[Ube Panic]: Out of memory allocating Scope chunk!\n");
        exit(1);
    }
    c->next = NULL;
    c->capacity = cap;
    return c;
}

static inline void ube_init(void) {
    if (current_scope.head == NULL) {
        current_scope.head = ube_chunk_alloc(UBE_SCOPE_CHUNK_SIZE);
        current_scope.current = current_scope.head;
        current_scope.top = 0;
        current_scope.total_chunks = 1;
    }
}

static inline UbeWatermark ube_scope_save(void) {
    UbeWatermark wm = { current_scope.current, current_scope.top };
    return wm;
}

static inline void ube_scope_restore(UbeWatermark wm) {
    if (!wm.chunk) return;
    UbeChunk* c = wm.chunk->next;
    while (c != NULL) {
        UbeChunk* next = c->next;
        free(c);
        c = next;
    }
    wm.chunk->next = NULL;
    current_scope.current = wm.chunk;
    current_scope.top = wm.top;
}

static inline void* ube_bump_alloc_unaligned(size_t size) {
    ube_init();
    if (current_scope.top + size > current_scope.current->capacity) {
        UbeChunk* new_c = ube_chunk_alloc(size);
        current_scope.current->next = new_c;
        current_scope.current = new_c;
        current_scope.top = 0;
        current_scope.total_chunks++;
    }
    void* ptr = &current_scope.current->memory[current_scope.top];
    current_scope.top += size;
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

static inline string ube_str_slice(string s, int32_t start, int32_t end) {
    if (start < 0) start = 0;
    if ((size_t)start > s.len) start = (int32_t)s.len;
    if (end < start) end = start;
    if ((size_t)end > s.len) end = (int32_t)s.len;
    string sub = { s.data + start, (size_t)(end - start) };
    return sub;
}

static inline uint8_t ube_str_index(string s, int32_t index) {
    if (index < 0 || (size_t)index >= s.len) return 0;
    return (uint8_t)s.data[index];
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
    p->capacity = capacity > 0 ? capacity : 16; \
    p->count = 0; \
    p->free_count = 0; \
    p->slots = (UbeSlot_##T*)calloc(p->capacity, sizeof(UbeSlot_##T)); \
    p->freelist = (uint32_t*)malloc(p->capacity * sizeof(uint32_t)); \
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

// --- 4. Thread-Safe Ring Buffer Channels ---
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
    ch->buffer = malloc((capacity > 0 ? capacity : 1) * elem_size);
    ch->elem_size = elem_size;
    ch->capacity = capacity > 0 ? capacity : 1;
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

// --- 5. Spawned Vine Trampoline ---
typedef struct {
    void* (*fn)(void*);
    void* arg;
} UbeSpawnArgs;

static void* ube_spawn_trampoline(void* raw) {
    UbeSpawnArgs* a = (UbeSpawnArgs*)raw;
    ube_init();
    UbeWatermark watermark = ube_scope_save();
    a->fn(a->arg);
    ube_scope_restore(watermark);
    free(a);
    return NULL;
}

static inline void ube_spawn(void* (*fn)(void*), void* arg) {
    pthread_t th;
    UbeSpawnArgs* a = (UbeSpawnArgs*)malloc(sizeof(UbeSpawnArgs));
    a->fn = fn;
    a->arg = arg;
    pthread_create(&th, NULL, ube_spawn_trampoline, a);
    pthread_detach(th);
}

// --- 6. Helper I/O ---
static inline void println_str(const char* s) { printf("%s\n", s); }
static inline void println(int32_t v) { printf("%d\n", v); }
static inline void ube_print_string(string s) { printf("%.*s\n", (int)s.len, s.data); }
static inline void ube_print_u32(uint32_t v) { printf("%u\n", v); }
static inline void ube_print_u8(uint8_t v) { printf("%u\n", (unsigned)v); }
static inline void ube_print_i64(int64_t v) { printf("%lld\n", (long long)v); }
static inline void ube_print_u64(uint64_t v) { printf("%llu\n", (unsigned long long)v); }
static inline void ube_print_f32(float v) { printf("%g\n", (double)v); }
static inline void ube_print_f64(double v) { printf("%g\n", v); }
static inline void ube_print_bool(bool v) { puts(v ? "true" : "false"); }
static inline void ube_print_ptr(const void* p) { printf("%p\n", p); }
#define UBE_PRINTLN(X) _Generic((X), \
    string: ube_print_string, \
    bool: ube_print_bool, \
    int32_t: println, uint32_t: ube_print_u32, uint8_t: ube_print_u8, \
    int64_t: ube_print_i64, uint64_t: ube_print_u64, \
    float: ube_print_f32, double: ube_print_f64, \
    default: ube_print_ptr \
)(X)

#endif // UBE_RUNTIME_H
