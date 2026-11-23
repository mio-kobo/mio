#include <sqlite3.h>
#include <stddef.h>
#include <stdint.h>
#include <string.h>

void *mio_sqlite_open_memory(void) {
    sqlite3 *db = NULL;
    if (sqlite3_open(":memory:", &db) != SQLITE_OK) { sqlite3_close(db); return NULL; }
    return db;
}

/* Runs one or more statements. Returns the SQLite result code (0 on success), -1 for a null handle. */
int32_t mio_sqlite_exec(void *db, const uint8_t *sql) {
    if (db == NULL || sql == NULL) return -1;
    return (int32_t)sqlite3_exec((sqlite3 *)db, (const char *)sql, NULL, NULL, NULL);
}

/* First column of the first row as an integer; returns -1000000000 - rc when the statement fails. */
int64_t mio_sqlite_query_int(void *db, const uint8_t *sql) {
    sqlite3_stmt *stmt = NULL;
    int64_t value = 0;
    int rc;
    if (db == NULL || sql == NULL) return -1000000000;
    rc = sqlite3_prepare_v2((sqlite3 *)db, (const char *)sql, -1, &stmt, NULL);
    if (rc != SQLITE_OK) return -1000000000 - rc;
    rc = sqlite3_step(stmt);
    if (rc == SQLITE_ROW) value = sqlite3_column_int64(stmt, 0);
    else { sqlite3_finalize(stmt); return -1000000000 - rc; }
    sqlite3_finalize(stmt);
    return value;
}

/* First column of the first row as text, copied into out (NUL terminated, truncated to cap - 1).
   Returns the text length, or a negative value on failure. */
int64_t mio_sqlite_query_text(void *db, const uint8_t *sql, uint8_t *out, size_t cap) {
    sqlite3_stmt *stmt = NULL;
    int rc;
    size_t n;
    const unsigned char *text;
    if (db == NULL || sql == NULL || out == NULL || cap == 0) return -1;
    rc = sqlite3_prepare_v2((sqlite3 *)db, (const char *)sql, -1, &stmt, NULL);
    if (rc != SQLITE_OK) return -1000 - rc;
    rc = sqlite3_step(stmt);
    if (rc != SQLITE_ROW) { sqlite3_finalize(stmt); return -1000 - rc; }
    text = sqlite3_column_text(stmt, 0);
    n = text == NULL ? 0 : strlen((const char *)text);
    if (n > cap - 1) n = cap - 1;
    if (n > 0) memcpy(out, text, n);
    out[n] = 0;
    sqlite3_finalize(stmt);
    return (int64_t)n;
}

void mio_sqlite_close(void *db) { if (db != NULL) sqlite3_close((sqlite3 *)db); }
