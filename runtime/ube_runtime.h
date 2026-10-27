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

// --- 1. Chunked Immutable Scopes (Arena Allocator with per-thread recycling) ---
typedef struct UbeChunk {
    struct UbeChunk* next;
    size_t capacity;
    uint8_t memory[];
} UbeChunk;

typedef struct {
    UbeChunk* head;
    UbeChunk* current;
    size_t top;
    size_t total_chunks;
    UbeChunk* freelist;
    size_t freelist_count;
} UbeScopeArena;

typedef struct {
    UbeChunk* chunk;
    size_t top;
} UbeWatermark;

static inline void ube_panic_capacity(int64_t capacity);

#if defined(__STDC_VERSION__) && __STDC_VERSION__ >= 201112L
static _Thread_local UbeScopeArena current_scope;
#elif defined(__GNUC__) || defined(__clang__)
static __thread UbeScopeArena current_scope;
#else
static UbeScopeArena current_scope;
#endif

static inline UbeChunk* ube_chunk_alloc(size_t min_capacity) {
    size_t cap = min_capacity > UBE_SCOPE_CHUNK_SIZE ? min_capacity : UBE_SCOPE_CHUNK_SIZE;
    if (cap > SIZE_MAX - sizeof(UbeChunk)) ube_panic_capacity((int64_t)cap);
    UbeChunk** link = &current_scope.freelist;
    while (*link) {
        UbeChunk* chunk = *link;
        if (chunk->capacity >= cap) {
            *link = chunk->next;
            chunk->next = NULL;
            if (current_scope.freelist_count) current_scope.freelist_count--;
            return chunk;
        }
        link = &chunk->next;
    }
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
    ube_init();
    UbeWatermark wm = { current_scope.current, current_scope.top };
    return wm;
}

static inline void ube_scope_restore(UbeWatermark wm) {
    if (!wm.chunk) return;
    UbeChunk* c = wm.chunk->next;
    if (c) {
        size_t count = 0;
        UbeChunk* tail = c;
        count++;
        while (tail->next) { tail = tail->next; count++; }
        tail->next = current_scope.freelist;
        current_scope.freelist = c;
        current_scope.freelist_count += count;
        if (current_scope.total_chunks >= count) current_scope.total_chunks -= count;
        wm.chunk->next = NULL;
    }
    current_scope.current = wm.chunk;
    current_scope.top = wm.top;
}

static inline void ube_scope_destroy(void) {
    UbeChunk* chunk = current_scope.head;
    while (chunk) { UbeChunk* next = chunk->next; free(chunk); chunk = next; }
    chunk = current_scope.freelist;
    while (chunk) { UbeChunk* next = chunk->next; free(chunk); chunk = next; }
    current_scope.head = NULL;
    current_scope.current = NULL;
    current_scope.top = 0;
    current_scope.total_chunks = 0;
    current_scope.freelist = NULL;
    current_scope.freelist_count = 0;
}

static inline void* ube_bump_alloc_unaligned(size_t size) {
    ube_init();
    if (size > current_scope.current->capacity - current_scope.top) {
        UbeChunk* new_c = ube_chunk_alloc(size);
        current_scope.current->next = new_c;
        current_scope.current = new_c;
        current_scope.top = 0;
        if (!current_scope.head) current_scope.head = new_c;
        current_scope.total_chunks++;
    }
    void* ptr = &current_scope.current->memory[current_scope.top];
    current_scope.top += size;
    return ptr;
}

static inline void* ube_bump_alloc(size_t size) {
    if (size > SIZE_MAX - 7) ube_panic_capacity((int64_t)size);
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
	if (a.len > SIZE_MAX - b.len - 1) ube_panic_capacity((int64_t)(a.len + b.len));
    size_t total = a.len + b.len;
    char* dest = (char*)ube_bump_alloc(total + 1);
    if (a.len) memcpy(dest, a.data, a.len);
    if (b.len) memcpy(dest + a.len, b.data, b.len);
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
    if (index < 0 || (size_t)index >= s.len) {
        fprintf(stderr, "[Ube Panic]: String index %d out of bounds (len %zu)\n", index, s.len);
        exit(101);
    }
    return (uint8_t)s.data[index];
}

static inline void ube_panic_bounds(int64_t index, size_t len, const char* file, int line) {
    fprintf(stderr, "\n[Ube Panic]: Index out of bounds at %s:%d\n", file, line);
    fprintf(stderr, "             Attempted index: %lld, length: %zu\n\n", (long long)index, len);
    exit(101);
}

static inline void ube_panic_capacity(int64_t capacity) {
    fprintf(stderr, "\n[Ube Panic]: Invalid or excessive container capacity: %lld\n\n", (long long)capacity);
    exit(101);
}

// GNU statement expressions preserve the indexed element as an lvalue, so
// checked indexing also protects writes such as values[index] = value.
#define UBE_SLICE_PTR(slice_expr, idx_expr, file_name, source_line) ({ \
    __auto_type _ube_slice = (slice_expr); \
    int64_t _ube_index = (int64_t)(idx_expr); \
    if (_ube_index < 0 || (uint64_t)_ube_index >= _ube_slice.len) \
        ube_panic_bounds(_ube_index, _ube_slice.len, (file_name), (source_line)); \
    &_ube_slice.data[_ube_index]; \
})

#define UBE_ARRAY_PTR(array_expr, idx_expr, array_len, file_name, source_line) ({ \
    __auto_type _ube_array = &(array_expr); \
    int64_t _ube_index = (int64_t)(idx_expr); \
    if (_ube_index < 0 || (uint64_t)_ube_index >= (size_t)(array_len)) \
        ube_panic_bounds(_ube_index, (size_t)(array_len), (file_name), (source_line)); \
    &_ube_array->data[_ube_index]; \
})

// --- 3. Generational Slot Pool (Pool[T]) ---
typedef struct {
    uint32_t index;
    uint32_t gen;
} UbeHandle;

#define DEFINE_UBE_POOL(T, N) \
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
} Pool_##N; \
static inline Pool_##N* Pool_##N##_new(int64_t requested_capacity) { \
    if (requested_capacity < 0 || (uint64_t)requested_capacity > UINT32_MAX || (uint64_t)requested_capacity > SIZE_MAX / sizeof(UbeSlot_##T) || (uint64_t)requested_capacity > SIZE_MAX / sizeof(uint32_t)) ube_panic_capacity(requested_capacity); \
    size_t capacity = requested_capacity > 0 ? (size_t)requested_capacity : 16; \
    Pool_##N* p = (Pool_##N*)malloc(sizeof(Pool_##N)); \
    if (!p) { fprintf(stderr, "[Ube Panic]: Out of memory allocating Pool\n"); exit(101); } \
    p->capacity = capacity; \
    p->count = 0; \
    p->free_count = 0; \
    p->slots = (UbeSlot_##T*)calloc(p->capacity, sizeof(UbeSlot_##T)); \
    p->freelist = (uint32_t*)malloc(p->capacity * sizeof(uint32_t)); \
    if (!p->slots || !p->freelist) { fprintf(stderr, "[Ube Panic]: Out of memory allocating Pool slots\n"); exit(101); } \
    return p; \
} \
static inline UbeHandle Pool_##N##_spawn(Pool_##N* p, T val) { \
    uint32_t idx; \
    if (p->free_count > 0) { \
        idx = p->freelist[--p->free_count]; \
    } else { \
        if (p->count >= p->capacity) { \
            if (p->capacity > UINT32_MAX / 2 || p->capacity > SIZE_MAX / (2 * sizeof(UbeSlot_##T)) || p->capacity > SIZE_MAX / (2 * sizeof(uint32_t))) ube_panic_capacity((int64_t)p->capacity); \
            p->capacity *= 2; \
            p->slots = (UbeSlot_##T*)realloc(p->slots, p->capacity * sizeof(UbeSlot_##T)); \
            p->freelist = (uint32_t*)realloc(p->freelist, p->capacity * sizeof(uint32_t)); \
            if (!p->slots || !p->freelist) { fprintf(stderr, "[Ube Panic]: Out of memory growing Pool\n"); exit(101); } \
        } \
        idx = (uint32_t)(p->count++); \
        p->slots[idx].gen = 1; \
    } \
    p->slots[idx].data = val; \
    p->slots[idx].active = true; \
    UbeHandle h = { idx, p->slots[idx].gen }; \
    return h; \
} \
static inline UbeOption_##N Pool_##N##_get(Pool_##N* p, UbeHandle h) { \
    UbeOption_##N result = { .has_val = false }; \
    if (h.index >= p->count || !p->slots[h.index].active || p->slots[h.index].gen != h.gen) return result; \
    result.has_val = true; \
    result.val = p->slots[h.index].data; \
    return result; \
} \
static inline bool Pool_##N##_despawn(Pool_##N* p, UbeHandle h) { \
    if (h.index >= p->count || !p->slots[h.index].active || p->slots[h.index].gen != h.gen) return false; \
    p->slots[h.index].active = false; \
    if (p->slots[h.index].gen != UINT32_MAX) { \
        p->slots[h.index].gen++; \
        p->freelist[p->free_count++] = h.index; \
    } \
    return true; \
}

// --- 4. Thread-Safe Ring Buffer Channels and event-driven select ---
typedef struct {
    pthread_mutex_t mutex;
    pthread_cond_t cond;
    bool notified;
} UbeSelectGroup;

typedef struct UbeSelectWaiter {
    UbeSelectGroup* group;
    struct UbeSelectWaiter* next;
} UbeSelectWaiter;

static inline void ube_select_group_init(UbeSelectGroup* group) {
    pthread_mutex_init(&group->mutex, NULL);
    pthread_cond_init(&group->cond, NULL);
    group->notified = false;
}

static inline void ube_select_group_destroy(UbeSelectGroup* group) {
    pthread_mutex_destroy(&group->mutex);
    pthread_cond_destroy(&group->cond);
}

static inline void ube_select_wait(UbeSelectGroup* group) {
    pthread_mutex_lock(&group->mutex);
    while (!group->notified) pthread_cond_wait(&group->cond, &group->mutex);
    group->notified = false;
    pthread_mutex_unlock(&group->mutex);
}

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
    UbeSelectWaiter* select_waiters;
} UbeRawChan;

static inline void ube_raw_chan_notify_select_locked(UbeRawChan* ch) {
    for (UbeSelectWaiter* waiter = ch->select_waiters; waiter; waiter = waiter->next) {
        UbeSelectGroup* group = waiter->group;
        pthread_mutex_lock(&group->mutex);
        group->notified = true;
        pthread_cond_signal(&group->cond);
        pthread_mutex_unlock(&group->mutex);
    }
}

static inline void ube_select_register(UbeRawChan* ch, UbeSelectWaiter* waiter, UbeSelectGroup* group) {
    waiter->group = group;
    pthread_mutex_lock(&ch->lock);
    waiter->next = ch->select_waiters;
    ch->select_waiters = waiter;
    pthread_mutex_unlock(&ch->lock);
}

static inline void ube_select_unregister(UbeRawChan* ch, UbeSelectWaiter* waiter) {
    pthread_mutex_lock(&ch->lock);
    UbeSelectWaiter** link = &ch->select_waiters;
    while (*link && *link != waiter) link = &(*link)->next;
    if (*link == waiter) *link = waiter->next;
    pthread_mutex_unlock(&ch->lock);
}

static inline bool ube_raw_chan_has_data_or_closed(UbeRawChan* ch) {
    pthread_mutex_lock(&ch->lock);
    bool ready = ch->count > 0 || ch->closed;
    pthread_mutex_unlock(&ch->lock);
    return ready;
}

static inline bool ube_raw_chan_is_closed(UbeRawChan* ch) {
    pthread_mutex_lock(&ch->lock);
    bool closed = ch->closed;
    pthread_mutex_unlock(&ch->lock);
    return closed;
}

static inline UbeRawChan* ube_raw_chan_new(int64_t requested_capacity, size_t elem_size) {
    if (requested_capacity < 0 || (uint64_t)requested_capacity > SIZE_MAX / (elem_size ? elem_size : 1)) ube_panic_capacity(requested_capacity);
    size_t capacity = requested_capacity > 0 ? (size_t)requested_capacity : 1;
    UbeRawChan* ch = (UbeRawChan*)malloc(sizeof(UbeRawChan));
    if (!ch) { fprintf(stderr, "[Ube Panic]: Out of memory allocating channel\n"); exit(101); }
    ch->buffer = malloc(capacity * elem_size);
    if (!ch->buffer) { fprintf(stderr, "[Ube Panic]: Out of memory allocating channel buffer\n"); exit(101); }
    ch->elem_size = elem_size;
    ch->capacity = capacity;
    ch->count = 0;
    ch->head = 0;
    ch->tail = 0;
    ch->closed = false;
    ch->select_waiters = NULL;
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
    ube_raw_chan_notify_select_locked(ch);
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
    ube_raw_chan_notify_select_locked(ch);
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
    ube_scope_destroy();
    free(a);
    return NULL;
}

static inline void ube_spawn(void* (*fn)(void*), void* arg) {
    pthread_t th;
    UbeSpawnArgs* a = (UbeSpawnArgs*)malloc(sizeof(UbeSpawnArgs));
    if (!a) {
        fprintf(stderr, "[Ube Panic]: Out of memory starting Vine\n");
        exit(101);
    }
    a->fn = fn;
    a->arg = arg;
    int status = pthread_create(&th, NULL, ube_spawn_trampoline, a);
    if (status != 0) {
        free(a);
        fprintf(stderr, "[Ube Panic]: Could not start Vine (error %d)\n", status);
        exit(101);
    }
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
