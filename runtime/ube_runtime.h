#ifndef UBE_RUNTIME_H
#define UBE_RUNTIME_H

#define _GNU_SOURCE
#include <stdio.h>
#include <stdlib.h>
#include <stdint.h>
#include <stdbool.h>
#include <string.h>
#include <math.h>
#include <errno.h>
#include <time.h>

/*
 * Ube runtime for WebAssembly.
 *
 * This header deliberately uses no POSIX-only headers (no pthread.h, dirent.h,
 * sys/stat.h or unistd.h). Everything here runs against Wasm linear memory and
 * the C standard library subset provided by WASI / Emscripten, with a native C
 * fallback so the same generated source can still be tested on a developer
 * machine.
 */

#define UBE_SCOPE_CHUNK_SIZE (64 * 1024)

// --- 1. Chunked Arenas for WebAssembly Linear Memory ---
typedef struct UbeChunk {
    struct UbeChunk* next;
    size_t capacity;
    uint8_t memory[];
} UbeChunk;

typedef struct UbeArenaLevel {
    UbeChunk* head;
    UbeChunk* current;
    size_t top;
    struct UbeArenaLevel* parent;
} UbeArenaLevel;

typedef struct {
    UbeChunk* head;
    UbeChunk* current;
    size_t top;
    size_t total_chunks;
    UbeChunk* freelist;
    size_t freelist_count;
    UbeArenaLevel* active_level;
} UbeScopeArena;

typedef struct {
    UbeChunk* chunk;
    size_t top;
} UbeWatermark;

static inline void ube_panic_capacity(int64_t capacity);

// Wasm instances are single-threaded by construction, so the current arena is
// module-local state rather than thread-local state.
static UbeScopeArena current_scope;

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
        fprintf(stderr, "[Ube Panic]: Wasm Linear Memory exhaustion!\n");
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
        current_scope.active_level = NULL;
    }
}

static inline void ube_arena_enter(UbeArenaLevel* level) {
    ube_init();
    level->parent = current_scope.active_level;
    level->head = ube_chunk_alloc(UBE_SCOPE_CHUNK_SIZE);
    level->current = level->head;
    level->top = 0;
    current_scope.active_level = level;
}

static inline void ube_arena_exit(UbeArenaLevel* level) {
    if (!level || !level->head) return;
    UbeChunk* c = level->head;
    UbeChunk* tail = c;
    size_t count = 1;
    while (tail->next) { tail = tail->next; count++; }
    tail->next = current_scope.freelist;
    current_scope.freelist = c;
    current_scope.freelist_count += count;
    if (current_scope.total_chunks >= count) current_scope.total_chunks -= count;
    current_scope.active_level = level->parent;
    level->head = NULL;
    level->current = NULL;
}

static inline void* ube_bump_alloc_parent(size_t size) {
    ube_init();
    if (size > SIZE_MAX - 7) ube_panic_capacity((int64_t)size);
    size = (size + 7) & ~7;
    UbeChunk** phead = &current_scope.head;
    UbeChunk** pcurrent = &current_scope.current;
    size_t* ptop = &current_scope.top;
    if (current_scope.active_level && current_scope.active_level->parent) {
        phead = &current_scope.active_level->parent->head;
        pcurrent = &current_scope.active_level->parent->current;
        ptop = &current_scope.active_level->parent->top;
    }
    if (size > (*pcurrent)->capacity - *ptop) {
        UbeChunk* new_c = ube_chunk_alloc(size);
        (*pcurrent)->next = new_c;
        *pcurrent = new_c;
        *ptop = 0;
        if (!*phead) *phead = new_c;
        current_scope.total_chunks++;
    }
    void* ptr = &(*pcurrent)->memory[*ptop];
    *ptop += size;
    return ptr;
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
    current_scope.active_level = NULL;
}

static inline void* ube_bump_alloc_unaligned(size_t size) {
    ube_init();
    UbeChunk** phead = &current_scope.head;
    UbeChunk** pcurrent = &current_scope.current;
    size_t* ptop = &current_scope.top;
    if (current_scope.active_level) {
        phead = &current_scope.active_level->head;
        pcurrent = &current_scope.active_level->current;
        ptop = &current_scope.active_level->top;
    }
    if (size > (*pcurrent)->capacity - *ptop) {
        UbeChunk* new_c = ube_chunk_alloc(size);
        (*pcurrent)->next = new_c;
        *pcurrent = new_c;
        *ptop = 0;
        if (!*phead) *phead = new_c;
        current_scope.total_chunks++;
    }
    void* ptr = &(*pcurrent)->memory[*ptop];
    *ptop += size;
    return ptr;
}

static inline void* ube_bump_alloc(size_t size) {
    if (size > SIZE_MAX - 7) ube_panic_capacity((int64_t)size);
    size = (size + 7) & ~7;
    return ube_bump_alloc_unaligned(size);
}

// --- 2. String & StringBuilder in Wasm ---
typedef struct {
    const char* data;
    size_t len;
} string;

static inline string ube_str_lit(const char* lit) {
    string s = { lit, lit ? strlen(lit) : 0 };
    return s;
}

static inline string ube_str_promote(string s) {
    if (!s.data || s.len == 0) return s;
    char* dest = (char*)ube_bump_alloc_parent(s.len + 1);
    memcpy(dest, s.data, s.len);
    dest[s.len] = '\0';
    return (string){ dest, s.len };
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

static inline bool ube_str_eq(string a, string b) {
    return a.len == b.len && (a.len == 0 || memcmp(a.data, b.data, a.len) == 0);
}
static inline bool ube_str_contains(string s, string sub) {
    if (sub.len == 0) return true;
    if (s.len < sub.len) return false;
    for (size_t i = 0; i <= s.len - sub.len; i++) if (memcmp(s.data + i, sub.data, sub.len) == 0) return true;
    return false;
}
static inline bool ube_str_starts_with(string s, string prefix) {
    return s.len >= prefix.len && (prefix.len == 0 || memcmp(s.data, prefix.data, prefix.len) == 0);
}
static inline bool ube_str_ends_with(string s, string suffix) {
    return s.len >= suffix.len && (suffix.len == 0 || memcmp(s.data + s.len - suffix.len, suffix.data, suffix.len) == 0);
}

typedef struct {
    char* data;
    size_t len;
    size_t cap;
} UbeStringBuilder;

static inline UbeStringBuilder ube_string_builder_new(int64_t initial_cap) {
    size_t cap = initial_cap > 0 ? (size_t)initial_cap : 16;
    char* data = (char*)ube_bump_alloc(cap + 1);
    data[0] = '\0';
    return (UbeStringBuilder){ .data = data, .len = 0, .cap = cap };
}

static inline void ube_string_builder_append_str(UbeStringBuilder* sb, string s) {
    if (s.len == 0) return;
    if (sb->len + s.len >= sb->cap) {
        size_t new_cap = (sb->cap + s.len) * 2;
        char* new_data = (char*)ube_bump_alloc(new_cap + 1);
        if (sb->len > 0) memcpy(new_data, sb->data, sb->len);
        sb->data = new_data;
        sb->cap = new_cap;
    }
    memcpy(sb->data + sb->len, s.data, s.len);
    sb->len += s.len;
    sb->data[sb->len] = '\0';
}

static inline void ube_string_builder_append_int(UbeStringBuilder* sb, int64_t val) {
    char buf[32];
    int n = snprintf(buf, sizeof(buf), "%lld", (long long)val);
    if (n > 0) {
        ube_string_builder_append_str(sb, (string){ buf, (size_t)n });
    }
}

static inline void ube_string_builder_append_i64(UbeStringBuilder* sb, int64_t val) {
    ube_string_builder_append_int(sb, val);
}

static inline void ube_string_builder_append_char(UbeStringBuilder* sb, int64_t val) {
    char c = (char)val;
    ube_string_builder_append_str(sb, (string){ &c, 1 });
}

static inline void ube_string_builder_append_bool(UbeStringBuilder* sb, bool val) {
    if (val) {
        ube_string_builder_append_str(sb, (string){ "true", 4 });
    } else {
        ube_string_builder_append_str(sb, (string){ "false", 5 });
    }
}

static inline string ube_string_builder_to_str(UbeStringBuilder* sb) {
    return (string){ .data = sb->data, .len = sb->len };
}

static inline void ube_sb_append_any_str(UbeStringBuilder* sb, string s) { ube_string_builder_append_str(sb, s); }
static inline void ube_sb_append_any_bool(UbeStringBuilder* sb, bool b) { ube_string_builder_append_bool(sb, b); }
static inline void ube_sb_append_any_i64(UbeStringBuilder* sb, int64_t v) { ube_string_builder_append_int(sb, v); }
static inline void ube_sb_append_any_u64(UbeStringBuilder* sb, uint64_t v) {
    char buf[32]; int n = snprintf(buf, sizeof(buf), "%llu", (unsigned long long)v);
    if (n > 0) ube_string_builder_append_str(sb, (string){ buf, (size_t)n });
}
static inline void ube_sb_append_any_f64(UbeStringBuilder* sb, double v) {
    char buf[64]; int n = snprintf(buf, sizeof(buf), "%g", v);
    if (n > 0) ube_string_builder_append_str(sb, (string){ buf, (size_t)n });
}

#define UBE_SB_APPEND(SB, X) _Generic((X), \
    string: ube_sb_append_any_str, \
    bool: ube_sb_append_any_bool, \
    float: ube_sb_append_any_f64, \
    double: ube_sb_append_any_f64, \
    unsigned int: ube_sb_append_any_u64, \
    unsigned long: ube_sb_append_any_u64, \
    unsigned long long: ube_sb_append_any_u64, \
    default: ube_sb_append_any_i64 \
)(SB, X)

// --- 2.1 Standard Result / Option / Slice Types ---
#ifndef UbeResult_string_string_DEFINED
#define UbeResult_string_string_DEFINED
typedef struct { bool ok; union { string val; string err; }; } UbeResult_string_string;
#endif
#ifndef UbeResult_bool_string_DEFINED
#define UbeResult_bool_string_DEFINED
typedef struct { bool ok; union { bool val; string err; }; } UbeResult_bool_string;
#endif
#ifndef UbeResult_i64_string_DEFINED
#define UbeResult_i64_string_DEFINED
typedef struct { bool ok; union { int64_t val; string err; }; } UbeResult_i64_string;
#endif
#ifndef UbeSlice_string_DEFINED
#define UbeSlice_string_DEFINED
typedef struct { string* data; size_t len; size_t cap; } UbeSlice_string;
#endif
#ifndef UbeResult_slice_string_string_DEFINED
#define UbeResult_slice_string_string_DEFINED
typedef struct { bool ok; union { UbeSlice_string val; string err; }; } UbeResult_slice_string_string;
#endif
#ifndef UbeOption_string_DEFINED
#define UbeOption_string_DEFINED
typedef struct { bool has_val; string val; } UbeOption_string;
#endif

static inline string ube_str_trim(string s) {
    if (s.len == 0) return s;
    size_t start = 0;
    while (start < s.len && (s.data[start] == ' ' || s.data[start] == '\t' || s.data[start] == '\n' || s.data[start] == '\r')) {
        start++;
    }
    size_t end = s.len;
    while (end > start && (s.data[end - 1] == ' ' || s.data[end - 1] == '\t' || s.data[end - 1] == '\n' || s.data[end - 1] == '\r')) {
        end--;
    }
    return (string){ s.data + start, end - start };
}

static inline int32_t ube_str_index_of(string s, string substr) {
    if (substr.len == 0) return 0;
    if (s.len < substr.len) return -1;
    for (size_t i = 0; i + substr.len <= s.len; i++) {
        if (memcmp(s.data + i, substr.data, substr.len) == 0) return (int32_t)i;
    }
    return -1;
}

static inline UbeSlice_string ube_str_split(string s, string delim) {
    if (delim.len == 0) {
        UbeSlice_string res;
        res.len = res.cap = s.len;
        res.data = s.len ? (string*)ube_bump_alloc(s.len * sizeof(string)) : NULL;
        for (size_t i = 0; i < s.len; i++) {
            res.data[i] = (string){ s.data + i, 1 };
        }
        return res;
    }
    size_t count = 1;
    for (size_t i = 0; i + delim.len <= s.len; ) {
        if (memcmp(s.data + i, delim.data, delim.len) == 0) {
            count++;
            i += delim.len;
        } else {
            i++;
        }
    }
    string* parts = (string*)ube_bump_alloc(count * sizeof(string));
    size_t idx = 0;
    size_t last = 0;
    for (size_t i = 0; i + delim.len <= s.len; ) {
        if (memcmp(s.data + i, delim.data, delim.len) == 0) {
            parts[idx++] = (string){ s.data + last, i - last };
            i += delim.len;
            last = i;
        } else {
            i++;
        }
    }
    parts[idx++] = (string){ s.data + last, s.len - last };
    return (UbeSlice_string){ .data = parts, .len = idx, .cap = idx };
}

static inline string ube_str_replace(string s, string old_s, string new_s) {
    if (old_s.len == 0 || s.len == 0) return s;
    size_t occurrences = 0;
    for (size_t i = 0; i + old_s.len <= s.len; ) {
        if (memcmp(s.data + i, old_s.data, old_s.len) == 0) {
            occurrences++;
            i += old_s.len;
        } else {
            i++;
        }
    }
    if (occurrences == 0) return s;
    if (new_s.len > old_s.len && occurrences > (SIZE_MAX - s.len - 1) / (new_s.len - old_s.len)) {
        ube_panic_capacity((int64_t)(s.len + occurrences));
    }
    size_t new_len = s.len + occurrences * (new_s.len - old_s.len);
    char* dest = (char*)ube_bump_alloc(new_len + 1);
    size_t src_i = 0, dest_i = 0;
    while (src_i < s.len) {
        if (src_i + old_s.len <= s.len && memcmp(s.data + src_i, old_s.data, old_s.len) == 0) {
            if (new_s.len) memcpy(dest + dest_i, new_s.data, new_s.len);
            dest_i += new_s.len;
            src_i += old_s.len;
        } else {
            dest[dest_i++] = s.data[src_i++];
        }
    }
    dest[new_len] = '\0';
    return (string){ dest, new_len };
}

static inline string ube_int_to_str(int64_t val) {
    char buf[32]; int n = snprintf(buf, sizeof(buf), "%lld", (long long)val);
    if (n < 0) n = 0;
    char* mem = (char*)ube_bump_alloc((size_t)n + 1); memcpy(mem, buf, (size_t)n); mem[n] = '\0';
    return (string){mem, (size_t)n};
}

static inline UbeResult_i64_string ube_str_to_int(string s) {
    UbeResult_i64_string r; char buf[64];
    if (!s.len || s.len >= sizeof(buf)) { r.ok = false; r.err = ube_str_lit("invalid integer format"); return r; }
    memcpy(buf, s.data, s.len); buf[s.len] = '\0'; char* end = NULL; errno = 0;
    long long v = strtoll(buf, &end, 10);
    if (errno || end == buf || *end) { r.ok = false; r.err = ube_str_lit("invalid integer format"); return r; }
    r.ok = true; r.val = (int64_t)v; return r;
}

// --- 2.2 Portable File, Path and Environment Utilities ---
// These use only the C standard library so they work under WASI as well as in
// the native test runner. Operations that require POSIX directory APIs are
// reported as unsupported rather than pulling unportable headers into Wasm.
static inline string ube_path_join(string a, string b) {
    if (a.len == 0) return b;
    if (b.len == 0) return a;
    bool a_ends_slash = a.data[a.len - 1] == '/';
    bool b_starts_slash = b.data[0] == '/';
    size_t extra = (a_ends_slash || b_starts_slash) ? 0 : 1;
    if (a_ends_slash && b_starts_slash) {
        b.data++;
        b.len--;
    }
    size_t total = a.len + extra + b.len;
    char* dest = (char*)ube_bump_alloc(total + 1);
    memcpy(dest, a.data, a.len);
    if (extra) dest[a.len] = '/';
    memcpy(dest + a.len + extra, b.data, b.len);
    dest[total] = '\0';
    return (string){ dest, total };
}

static inline UbeResult_string_string ube_read_file(string path) {
    UbeResult_string_string r; char name[1024];
    if (path.len >= sizeof(name)) { r.ok = false; r.err = ube_str_lit("path too long"); return r; }
    memcpy(name, path.data, path.len); name[path.len] = '\0'; FILE* f = fopen(name, "rb");
    if (!f) { r.ok = false; r.err = ube_str_lit("cannot open file"); return r; }
    if (fseek(f, 0, SEEK_END) != 0) { fclose(f); r.ok = false; r.err = ube_str_lit("cannot seek file"); return r; }
    long size = ftell(f); if (size < 0 || fseek(f, 0, SEEK_SET) != 0) { fclose(f); r.ok = false; r.err = ube_str_lit("cannot read file"); return r; }
    char* data = (char*)ube_bump_alloc((size_t)size + 1); size_t got = fread(data, 1, (size_t)size, f); fclose(f); data[got] = '\0';
    r.ok = true; r.val = (string){data, got}; return r;
}

static inline UbeResult_bool_string ube_write_file(string path, string content) {
    UbeResult_bool_string r; char name[1024];
    if (path.len >= sizeof(name)) { r.ok = false; r.err = ube_str_lit("path too long"); return r; }
    memcpy(name, path.data, path.len); name[path.len] = '\0'; FILE* f = fopen(name, "wb");
    if (!f) { r.ok = false; r.err = ube_str_lit("cannot open file for writing"); return r; }
    size_t n = fwrite(content.data, 1, content.len, f); int close_status = fclose(f); bool ok = n == content.len && close_status == 0;
    r.ok = ok; if (ok) r.val = true; else r.err = ube_str_lit("cannot write file"); return r;
}

static inline bool ube_file_exists(string path) {
    char buf[1024];
    if (path.len >= sizeof(buf)) return false;
    memcpy(buf, path.data, path.len);
    buf[path.len] = '\0';
    FILE* f = fopen(buf, "rb");
    if (f) {
        fclose(f);
        return true;
    }
    return false;
}

static inline UbeResult_bool_string ube_file_remove(string path) {
    UbeResult_bool_string res;
    char buf[1024];
    if (path.len >= sizeof(buf)) {
        res.ok = false;
        res.err = ube_str_lit("path too long");
        return res;
    }
    memcpy(buf, path.data, path.len);
    buf[path.len] = '\0';
    if (remove(buf) != 0) {
        res.ok = false;
        res.err = ube_str_lit("cannot remove file");
        return res;
    }
    res.ok = true;
    res.val = true;
    return res;
}

#define UBE_ENV_MAX 64
#define UBE_ENV_KEY_MAX 128
#define UBE_ENV_VAL_MAX 512
typedef struct {
    char key[UBE_ENV_KEY_MAX];
    char val[UBE_ENV_VAL_MAX];
    bool used;
} UbeEnvSlot;
static UbeEnvSlot ube_env_slots[UBE_ENV_MAX];

static inline UbeEnvSlot* ube_env_find_slot(const char* key) {
    for (int i = 0; i < UBE_ENV_MAX; i++) {
        if (ube_env_slots[i].used && strcmp(ube_env_slots[i].key, key) == 0) return &ube_env_slots[i];
    }
    return NULL;
}

static inline bool ube_env_set(string key, string val) {
    if (key.len >= UBE_ENV_KEY_MAX || val.len >= UBE_ENV_VAL_MAX) return false;
    char k[UBE_ENV_KEY_MAX];
    memcpy(k, key.data, key.len);
    k[key.len] = '\0';
    UbeEnvSlot* slot = ube_env_find_slot(k);
    if (!slot) {
        for (int i = 0; i < UBE_ENV_MAX; i++) {
            if (!ube_env_slots[i].used) { slot = &ube_env_slots[i]; break; }
        }
    }
    if (!slot) return false;
    memcpy(slot->key, key.data, key.len);
    slot->key[key.len] = '\0';
    memcpy(slot->val, val.data, val.len);
    slot->val[val.len] = '\0';
    slot->used = true;
    return true;
}

static inline UbeOption_string ube_env_get(string key) {
    char buf[UBE_ENV_KEY_MAX];
    if (key.len >= sizeof(buf)) return (UbeOption_string){ .has_val = false };
    memcpy(buf, key.data, key.len);
    buf[key.len] = '\0';
    UbeEnvSlot* slot = ube_env_find_slot(buf);
    if (slot) return (UbeOption_string){ .has_val = true, .val = ube_str_lit(slot->val) };
    const char* val = getenv(buf);
    if (!val) return (UbeOption_string){ .has_val = false };
    return (UbeOption_string){ .has_val = true, .val = ube_str_lit(val) };
}

static inline UbeResult_slice_string_string ube_dir_list(string path) {
    (void)path;
    UbeResult_slice_string_string res;
    res.ok = false;
    res.err = ube_str_lit("dir_list is unavailable in the sandboxed Wasm runtime");
    return res;
}

static inline UbeResult_bool_string ube_dir_create(string path) {
    (void)path;
    UbeResult_bool_string res;
    res.ok = false;
    res.err = ube_str_lit("dir_create is unavailable in the sandboxed Wasm runtime");
    return res;
}

static inline int32_t ube_cmd_run(string cmd) {
    (void)cmd;
    return -1;
}

static int ube_global_argc = 0;
static char** ube_global_argv = NULL;
static inline void ube_init_args(int argc, char** argv) { ube_global_argc = argc; ube_global_argv = argv; }
static inline UbeSlice_string ube_os_args(void) {
    UbeSlice_string s; s.len = s.cap = (size_t)ube_global_argc;
    s.data = s.len ? (string*)ube_bump_alloc(s.len * sizeof(string)) : NULL;
    for (size_t i = 0; i < s.len; i++) s.data[i] = ube_str_lit(ube_global_argv[i]);
    return s;
}

// --- 3. Generational Handle Pools (Pool[T]) ---
typedef struct {
    uint32_t index;
    uint32_t gen;
} UbeHandle;

#define DEFINE_UBE_POOL(T, N) \
typedef struct { T data; uint32_t gen; bool active; } UbeSlot_##N; \
typedef struct { UbeSlot_##N* slots; uint32_t* freelist; size_t capacity; size_t count; size_t free_count; } Pool_##N; \
static inline Pool_##N* Pool_##N##_new(int64_t requested_capacity) { \
    if (requested_capacity < 0 || (uint64_t)requested_capacity > UINT32_MAX || (uint64_t)requested_capacity > SIZE_MAX / sizeof(UbeSlot_##N) || (uint64_t)requested_capacity > SIZE_MAX / sizeof(uint32_t)) ube_panic_capacity(requested_capacity); \
    size_t capacity = requested_capacity > 0 ? (size_t)requested_capacity : 16; \
    Pool_##N* p = (Pool_##N*)malloc(sizeof(Pool_##N)); \
    if (!p) { fprintf(stderr, "[Ube Panic]: Wasm Linear Memory exhaustion allocating Pool\n"); exit(101); } \
    p->capacity = capacity; p->count = 0; p->free_count = 0; \
    p->slots = (UbeSlot_##N*)calloc(p->capacity, sizeof(UbeSlot_##N)); \
    p->freelist = (uint32_t*)malloc(p->capacity * sizeof(uint32_t)); \
    if (!p->slots || !p->freelist) { fprintf(stderr, "[Ube Panic]: Wasm Linear Memory exhaustion allocating Pool slots\n"); exit(101); } \
    return p; \
} \
static inline UbeHandle Pool_##N##_spawn(Pool_##N* p, T val) { \
    uint32_t idx; \
    if (p->free_count > 0) { idx = p->freelist[--p->free_count]; } \
    else { \
        if (p->count >= p->capacity) { \
            if (p->capacity > UINT32_MAX / 2 || p->capacity > SIZE_MAX / (2 * sizeof(UbeSlot_##N)) || p->capacity > SIZE_MAX / (2 * sizeof(uint32_t))) ube_panic_capacity((int64_t)p->capacity); \
            p->capacity *= 2; \
            p->slots = (UbeSlot_##N*)realloc(p->slots, p->capacity * sizeof(UbeSlot_##N)); \
            p->freelist = (uint32_t*)realloc(p->freelist, p->capacity * sizeof(uint32_t)); \
            if (!p->slots || !p->freelist) { fprintf(stderr, "[Ube Panic]: Wasm Linear Memory exhaustion growing Pool\n"); exit(101); } \
        } \
        idx = (uint32_t)(p->count++); \
        p->slots[idx].gen = 1; \
    } \
    p->slots[idx].data = val; p->slots[idx].active = true; \
    return (UbeHandle){ idx, p->slots[idx].gen }; \
} \
static inline UbeOption_##N Pool_##N##_get(Pool_##N* p, UbeHandle h) { \
    UbeOption_##N result = { .has_val = false }; \
    if (h.index >= p->count || !p->slots[h.index].active || p->slots[h.index].gen != h.gen) return result; \
    result.has_val = true; result.val = p->slots[h.index].data; return result; \
} \
static inline bool Pool_##N##_despawn(Pool_##N* p, UbeHandle h) { \
    if (h.index >= p->count || !p->slots[h.index].active || p->slots[h.index].gen != h.gen) return false; \
    p->slots[h.index].active = false; \
    if (p->slots[h.index].gen == UINT32_MAX) p->slots[h.index].gen = 0; \
    p->slots[h.index].gen++; p->freelist[p->free_count++] = h.index; return true; \
}

// --- 4. Wasm Task & Message Passing Channels (Shared-Nothing) ---
typedef struct {
    bool notified;
} UbeSelectGroup;

typedef struct UbeSelectWaiter {
    UbeSelectGroup* group;
    struct UbeSelectWaiter* next;
} UbeSelectWaiter;

static inline void ube_select_group_init(UbeSelectGroup* group) { group->notified = false; }
static inline void ube_select_group_destroy(UbeSelectGroup* group) { (void)group; }
static inline void ube_run_pending(void);
static inline void ube_select_wait(UbeSelectGroup* group);

typedef struct {
    void* buffer;
    size_t elem_size;
    size_t capacity;
    size_t count;
    size_t head;
    size_t tail;
    bool closed;
    UbeSelectWaiter* select_waiters;
} UbeRawChan;

static inline void ube_select_register(UbeRawChan* ch, UbeSelectWaiter* waiter, UbeSelectGroup* group) {
    waiter->group = group;
    waiter->next = ch->select_waiters;
    ch->select_waiters = waiter;
}

static inline void ube_select_unregister(UbeRawChan* ch, UbeSelectWaiter* waiter) {
    UbeSelectWaiter** link = &ch->select_waiters;
    while (*link && *link != waiter) link = &(*link)->next;
    if (*link == waiter) *link = waiter->next;
}

static inline bool ube_raw_chan_has_data(UbeRawChan* ch) { return ch->count > 0; }
static inline bool ube_raw_chan_is_closed(UbeRawChan* ch) { return ch->closed; }

static inline UbeRawChan* ube_raw_chan_new(int64_t requested_capacity, size_t elem_size) {
    if (requested_capacity < 0 || (uint64_t)requested_capacity > SIZE_MAX / (elem_size ? elem_size : 1)) ube_panic_capacity(requested_capacity);
    size_t capacity = requested_capacity > 0 ? (size_t)requested_capacity : 1;
    UbeRawChan* ch = (UbeRawChan*)malloc(sizeof(UbeRawChan));
    if (!ch) { fprintf(stderr, "[Ube Panic]: Wasm Linear Memory exhaustion allocating channel\n"); exit(101); }
    ch->buffer = malloc(capacity * (elem_size ? elem_size : 1));
    if (!ch->buffer) { fprintf(stderr, "[Ube Panic]: Wasm Linear Memory exhaustion allocating channel buffer\n"); exit(101); }
    ch->elem_size = elem_size;
    ch->capacity = capacity;
    ch->count = 0;
    ch->head = 0;
    ch->tail = 0;
    ch->closed = false;
    ch->select_waiters = NULL;
    return ch;
}

static inline void ube_raw_chan_send(UbeRawChan* ch, const void* val) {
    if (ch->closed || ch->count >= ch->capacity) return;
    memcpy((uint8_t*)ch->buffer + (ch->tail * ch->elem_size), val, ch->elem_size);
    ch->tail = (ch->tail + 1) % ch->capacity;
    ch->count++;
    for (UbeSelectWaiter* w = ch->select_waiters; w; w = w->next) {
        w->group->notified = true;
    }
}

static inline bool ube_raw_chan_recv(UbeRawChan* ch, void* out) {
    if (ch->count == 0) ube_run_pending();
    if (ch->count == 0) return false;
    memcpy(out, (uint8_t*)ch->buffer + (ch->head * ch->elem_size), ch->elem_size);
    ch->head = (ch->head + 1) % ch->capacity;
    ch->count--;
    return true;
}

static inline bool ube_raw_chan_try_recv(UbeRawChan* ch, void* out) {
    return ube_raw_chan_recv(ch, out);
}

static inline void ube_raw_chan_close(UbeRawChan* ch) {
    ch->closed = true;
    for (UbeSelectWaiter* w = ch->select_waiters; w; w = w->next) {
        w->group->notified = true;
    }
}

// --- 5. Cooperative Wasm Tasks ---
typedef struct UbeFiberTask {
    void* (*fn)(void*);
    void* arg;
    struct UbeFiberTask* next;
} UbeFiberTask;

static UbeFiberTask* ube_task_head = NULL;
static UbeFiberTask* ube_task_tail = NULL;

static inline void ube_run_pending(void) {
    while (ube_task_head) {
        UbeFiberTask* task = ube_task_head;
        ube_task_head = task->next;
        if (!ube_task_head) ube_task_tail = NULL;
        UbeWatermark mark = ube_scope_save();
        task->fn(task->arg);
        ube_scope_restore(mark);
        free(task);
    }
}

static inline void ube_spawn(void* (*fn)(void*), void* arg) {
    UbeFiberTask* task = (UbeFiberTask*)malloc(sizeof(UbeFiberTask));
    if (!task) { fprintf(stderr, "[Ube Panic]: Wasm task allocation failed\n"); exit(101); }
    task->fn = fn;
    task->arg = arg;
    task->next = NULL;
    if (ube_task_tail) ube_task_tail->next = task;
    else ube_task_head = task;
    ube_task_tail = task;
}

static inline void ube_yield(void) { ube_run_pending(); }

static inline void ube_select_wait(UbeSelectGroup* group) {
    group->notified = false;
    ube_run_pending();
}

// --- 6. Standard Wasm Utilities & Math ---
static inline int64_t ube_abs(int64_t n) { return n < 0 ? -n : n; }
static inline int64_t ube_min(int64_t a, int64_t b) { return a < b ? a : b; }
static inline int64_t ube_max(int64_t a, int64_t b) { return a > b ? a : b; }
static inline double ube_sqrt(double n) { return sqrt(n); }
static inline double ube_pow(double b, double e) { return pow(b, e); }
static inline double ube_floor(double n) { return floor(n); }
static inline double ube_ceil(double n) { return ceil(n); }
static inline int64_t ube_time_now_ms(void) {
    struct timespec ts;
    timespec_get(&ts, TIME_UTC);
    return (int64_t)ts.tv_sec * 1000 + ts.tv_nsec / 1000000;
}
static inline void ube_sleep_ms(int32_t ms) { (void)ms; ube_run_pending(); }
static inline void ube_exit_process(int32_t code) { exit(code); }

static inline void ube_panic_bounds(int64_t index, size_t len, const char* file, int line) {
    fprintf(stderr, "\n[Ube Panic]: Index %lld out of bounds (len %zu) at %s:%d\n\n", (long long)index, len, file, line);
    exit(101);
}

static inline void ube_panic_capacity(int64_t capacity) {
    fprintf(stderr, "\n[Ube Panic]: Container capacity invalid or overflow: %lld\n\n", (long long)capacity);
    exit(101);
}

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

#define UBE_STR_INDEX(str_expr, idx_expr, file_name, source_line) ({ \
    string _ube_str = (str_expr); \
    int64_t _ube_index = (int64_t)(idx_expr); \
    if (_ube_index < 0 || (uint64_t)_ube_index >= _ube_str.len) \
        ube_panic_bounds(_ube_index, _ube_str.len, (file_name), (source_line)); \
    (uint8_t)_ube_str.data[_ube_index]; \
})

// --- 7. Polymorphic Output ---
static inline void println_str(const char* s) { puts(s); }
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
static inline void ube_print_raw_string(string s) { printf("%.*s", (int)s.len, s.data); }
static inline void ube_print_raw_int(int32_t v) { printf("%d", v); }
static inline void ube_print_raw_u32(uint32_t v) { printf("%u", v); }
static inline void ube_print_raw_u8(uint8_t v) { printf("%u", (unsigned)v); }
static inline void ube_print_raw_i64(int64_t v) { printf("%lld", (long long)v); }
static inline void ube_print_raw_u64(uint64_t v) { printf("%llu", (unsigned long long)v); }
static inline void ube_print_raw_f32(float v) { printf("%g", (double)v); }
static inline void ube_print_raw_f64(double v) { printf("%g", v); }
static inline void ube_print_raw_bool(bool v) { fputs(v ? "true" : "false", stdout); }

#define UBE_PRINTLN(X) _Generic((X), \
    string: ube_print_string, \
    bool: ube_print_bool, \
    int32_t: println, uint32_t: ube_print_u32, uint8_t: ube_print_u8, \
    int64_t: ube_print_i64, uint64_t: ube_print_u64, \
    float: ube_print_f32, double: ube_print_f64, \
    default: ube_print_ptr \
)(X)

#define UBE_PRINT(X) _Generic((X), \
    string: ube_print_raw_string, bool: ube_print_raw_bool, \
    int32_t: ube_print_raw_int, uint32_t: ube_print_raw_u32, uint8_t: ube_print_raw_u8, \
    int64_t: ube_print_raw_i64, uint64_t: ube_print_raw_u64, \
    float: ube_print_raw_f32, double: ube_print_raw_f64, \
    default: ube_print_ptr \
)(X)

#endif // UBE_RUNTIME_H
