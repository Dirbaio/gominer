#ifndef DECRED_H
#define DECRED_H

#include <stdint.h>

#ifdef __cplusplus
extern "C" {
#endif /* __cplusplus */

void decred_blake3_hash(const uint32_t dimgrid, const uint32_t threads, uint32_t *midstate, uint32_t *lastblock, uint32_t *out);

#ifdef __cplusplus
}
#endif /* __cplusplus */

#endif /* DECRED_H */
