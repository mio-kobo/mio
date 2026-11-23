#include <openssl/evp.h>
#include <stddef.h>
#include <stdint.h>

/* SHA-256 of data[0..len) into out[0..32). Returns 0 on success, negative on failure. */
int32_t mio_sha256(const uint8_t *data, size_t len, uint8_t *out) {
    EVP_MD_CTX *ctx;
    unsigned int outlen = 0;
    int32_t rc = -3;
    if (data == NULL || out == NULL) return -1;
    ctx = EVP_MD_CTX_new();
    if (ctx == NULL) return -2;
    if (EVP_DigestInit_ex(ctx, EVP_sha256(), NULL) == 1 &&
        EVP_DigestUpdate(ctx, data, len) == 1 &&
        EVP_DigestFinal_ex(ctx, out, &outlen) == 1 && outlen == 32) rc = 0;
    EVP_MD_CTX_free(ctx);
    return rc;
}
