// Copyright (c) 2023 The Decred developers.
//
// Decred BLAKE3 midstate-based CUDA kernel

// Written and optimized by Dave Collins Sep 2023.

#include <stdint.h>

#if defined(_WIN32)
#define DLLEXPORT __declspec(dllexport)
#else
#define DLLEXPORT
#endif /* _WIN32 */

#define MAX_OUTPUT_RESULTS 32

// Written and optimized by Dave Collins Sep 2023.
#if __CUDACC_VER_MAJOR__ >= 10
#define ROTR(v, n) __funnelshift_rc((v), (v), n)
#else
#define ROTR(v, n) ((v) >> n) | ((v) << (32 - n))
#endif


__global__
void search(
    uint32_t *output,
    // Midstate.
    const uint32_t cv0,
    const uint32_t cv1,
    const uint32_t cv2,
    const uint32_t cv3,
    const uint32_t cv4,
    const uint32_t cv5,
    const uint32_t cv6,
    const uint32_t cv7,

    // Final 52 bytes of data.
    const uint32_t m0,
    const uint32_t m1,
    const uint32_t m2,
    // const uint32_t m3 : nonce
    const uint32_t m4,
    const uint32_t m5,
    const uint32_t m6,
    const uint32_t m7,
    const uint32_t m8,
    const uint32_t m9,
    const uint32_t m10,
    const uint32_t m11,
    const uint32_t m12)
{
    // Nonce.
    const uint32_t m3 = blockDim.x * blockIdx.x + threadIdx.x;

    // BLAKE3 init vectors.
    const uint32_t iv0 = 0x6a09e667ul;
    const uint32_t iv1 = 0xbb67ae85ul;
    const uint32_t iv2 = 0x3c6ef372ul;
    const uint32_t iv3 = 0xa54ff53aul;
    // const uint32_t iv4 = 0x510e527ful;
    // const uint32_t iv5 = 0x9b05688cul;
    // const uint32_t iv6 = 0x1f83d9abul;
    // const uint32_t iv7 = 0x5be0cd19ul;

    // Internal compression func state.
    uint32_t v0, v1, v2, v3, v4, v5, v6, v7;
    uint32_t v8, v9, v10, v11, v12, v13, v14, v15;

    // Do the initialization and first round together.
    // Round 1.
    v0 = cv0 + cv4 + m0; v12 = ROTR(v0, 16); v8 = iv0 + v12; v4 = ROTR(cv4 ^ v8, 12); v0 += v4 + m1; v12 = ROTR(v12 ^ v0, 8); v8 += v12; v4 = ROTR(v4 ^ v8, 7);
    v1 = cv1 + cv5 + m2; v13 = ROTR(v1, 16); v9 = iv1 + v13; v5 = ROTR(cv5 ^ v9, 12); v1 += v5 + m3; v13 = ROTR(v13 ^ v1, 8); v9 += v13; v5 = ROTR(v5 ^ v9, 7);
    v2 = cv2 + cv6 + m4; v14 = ROTR(52 ^ v2, 16); v10 = iv2 + v14; v6 = ROTR(cv6 ^ v10, 12); v2 += v6 + m5; v14 = ROTR(v14 ^ v2, 8); v10 += v14; v6 = ROTR(v6 ^ v10, 7);
    v3 = cv3 + cv7 + m6; v15 = ROTR(10 ^ v3, 16); v11 = iv3 + v15; v7 = ROTR(cv7 ^ v11, 12); v3 += v7 + m7; v15 = ROTR(v15 ^ v3, 8); v11 += v15; v7 = ROTR(v7 ^ v11, 7);
    v0 = v0 + v5 + m8; v15 = ROTR(v15 ^ v0, 16); v10 += v15; v5 = ROTR(v5 ^ v10, 12); v0 += v5 + m9; v15 = ROTR(v15 ^ v0, 8); v10 += v15; v5 = ROTR(v5 ^ v10, 7);
    v1 = v1 + v6 + m10; v12 = ROTR(v12 ^ v1, 16); v11 += v12; v6 = ROTR(v6 ^ v11, 12); v1 += v6 + m11; v12 = ROTR(v12 ^ v1, 8); v11 += v12; v6 = ROTR(v6 ^ v11, 7);
    v2 = v2 + v7 + m12; v13 = ROTR(v13 ^ v2, 16); v8 += v13; v7 = ROTR(v7 ^ v8, 12); v2 += v7; v13 = ROTR(v13 ^ v2, 8); v8 += v13; v7 = ROTR(v7 ^ v8, 7);
    v3 = v3 + v4; v14 = ROTR(v14 ^ v3, 16); v9 += v14; v4 = ROTR(v4 ^ v9, 12); v3 += v4; v14 = ROTR(v14 ^ v3, 8); v9 += v14; v4 = ROTR(v4 ^ v9, 7);

    // Round 2 with message word permutation.
    v0 = v0 + v4 + m2; v12 = ROTR(v12 ^ v0, 16); v8 = v8 + v12; v4 = ROTR(v4 ^ v8, 12); v0 += v4 + m6; v12 = ROTR(v12 ^ v0, 8); v8 += v12; v4 = ROTR(v4 ^ v8, 7);
    v1 = v1 + v5 + m3; v13 = ROTR(v13 ^ v1, 16); v9 = v9 + v13; v5 = ROTR(v5 ^ v9, 12); v1 += v5 + m10; v13 = ROTR(v13 ^ v1, 8); v9 += v13; v5 = ROTR(v5 ^ v9, 7);
    v2 = v2 + v6 + m7; v14 = ROTR(v14 ^ v2, 16); v10 += v14; v6 = ROTR(v6 ^ v10, 12); v2 += v6 + m0; v14 = ROTR(v14 ^ v2, 8); v10 += v14; v6 = ROTR(v6 ^ v10, 7);
    v3 = v3 + v7 + m4; v15 = ROTR(v15 ^ v3, 16); v11 += v15; v7 = ROTR(v7 ^ v11, 12); v3 += v7; v15 = ROTR(v15 ^ v3, 8); v11 += v15; v7 = ROTR(v7 ^ v11, 7);
    v0 = v0 + v5 + m1; v15 = ROTR(v15 ^ v0, 16); v10 += v15; v5 = ROTR(v5 ^ v10, 12); v0 += v5 + m11; v15 = ROTR(v15 ^ v0, 8); v10 += v15; v5 = ROTR(v5 ^ v10, 7);
    v1 = v1 + v6 + m12; v12 = ROTR(v12 ^ v1, 16); v11 += v12; v6 = ROTR(v6 ^ v11, 12); v1 += v6 + m5; v12 = ROTR(v12 ^ v1, 8); v11 += v12; v6 = ROTR(v6 ^ v11, 7);
    v2 = v2 + v7 + m9; v13 = ROTR(v13 ^ v2, 16); v8 += v13; v7 = ROTR(v7 ^ v8, 12); v2 += v7; v13 = ROTR(v13 ^ v2, 8); v8 += v13; v7 = ROTR(v7 ^ v8, 7);
    v3 = v3 + v4; v14 = ROTR(v14 ^ v3, 16); v9 += v14; v4 = ROTR(v4 ^ v9, 12); v3 += v4 + m8; v14 = ROTR(v14 ^ v3, 8); v9 += v14; v4 = ROTR(v4 ^ v9, 7);

    // Round 3 with message word permutation.
    v0 = v0 + v4 + m3; v12 = ROTR(v12 ^ v0, 16); v8 = v8 + v12; v4 = ROTR(v4 ^ v8, 12); v0 += v4 + m4; v12 = ROTR(v12 ^ v0, 8); v8 += v12; v4 = ROTR(v4 ^ v8, 7);
    v1 = v1 + v5 + m10; v13 = ROTR(v13 ^ v1, 16); v9 = v9 + v13; v5 = ROTR(v5 ^ v9, 12); v1 += v5 + m12; v13 = ROTR(v13 ^ v1, 8); v9 += v13; v5 = ROTR(v5 ^ v9, 7);
    v2 = v2 + v6; v14 = ROTR(v14 ^ v2, 16); v10 += v14; v6 = ROTR(v6 ^ v10, 12); v2 += v6 + m2; v14 = ROTR(v14 ^ v2, 8); v10 += v14; v6 = ROTR(v6 ^ v10, 7);
    v3 = v3 + v7 + m7; v15 = ROTR(v15 ^ v3, 16); v11 += v15; v7 = ROTR(v7 ^ v11, 12); v3 += v7; v15 = ROTR(v15 ^ v3, 8); v11 += v15; v7 = ROTR(v7 ^ v11, 7);
    v0 = v0 + v5 + m6; v15 = ROTR(v15 ^ v0, 16); v10 += v15; v5 = ROTR(v5 ^ v10, 12); v0 += v5 + m5; v15 = ROTR(v15 ^ v0, 8); v10 += v15; v5 = ROTR(v5 ^ v10, 7);
    v1 = v1 + v6 + m9; v12 = ROTR(v12 ^ v1, 16); v11 += v12; v6 = ROTR(v6 ^ v11, 12); v1 += v6 + m0; v12 = ROTR(v12 ^ v1, 8); v11 += v12; v6 = ROTR(v6 ^ v11, 7);
    v2 = v2 + v7 + m11; v13 = ROTR(v13 ^ v2, 16); v8 += v13; v7 = ROTR(v7 ^ v8, 12); v2 += v7; v13 = ROTR(v13 ^ v2, 8); v8 += v13; v7 = ROTR(v7 ^ v8, 7);
    v3 = v3 + v4 + m8; v14 = ROTR(v14 ^ v3, 16); v9 += v14; v4 = ROTR(v4 ^ v9, 12); v3 += v4 + m1; v14 = ROTR(v14 ^ v3, 8); v9 += v14; v4 = ROTR(v4 ^ v9, 7);

    // Round 4 with message word permutation.
    v0 = v0 + v4 + m10; v12 = ROTR(v12 ^ v0, 16); v8 = v8 + v12; v4 = ROTR(v4 ^ v8, 12); v0 += v4 + m7; v12 = ROTR(v12 ^ v0, 8); v8 += v12; v4 = ROTR(v4 ^ v8, 7);
    v1 = v1 + v5 + m12; v13 = ROTR(v13 ^ v1, 16); v9 = v9 + v13; v5 = ROTR(v5 ^ v9, 12); v1 += v5 + m9; v13 = ROTR(v13 ^ v1, 8); v9 += v13; v5 = ROTR(v5 ^ v9, 7);
    v2 = v2 + v6; v14 = ROTR(v14 ^ v2, 16); v10 += v14; v6 = ROTR(v6 ^ v10, 12); v2 += v6 + m3; v14 = ROTR(v14 ^ v2, 8); v10 += v14; v6 = ROTR(v6 ^ v10, 7);
    v3 = v3 + v7; v15 = ROTR(v15 ^ v3, 16); v11 += v15; v7 = ROTR(v7 ^ v11, 12); v3 += v7; v15 = ROTR(v15 ^ v3, 8); v11 += v15; v7 = ROTR(v7 ^ v11, 7);
    v0 = v0 + v5 + m4; v15 = ROTR(v15 ^ v0, 16); v10 += v15; v5 = ROTR(v5 ^ v10, 12); v0 += v5 + m0; v15 = ROTR(v15 ^ v0, 8); v10 += v15; v5 = ROTR(v5 ^ v10, 7);
    v1 = v1 + v6 + m11; v12 = ROTR(v12 ^ v1, 16); v11 += v12; v6 = ROTR(v6 ^ v11, 12); v1 += v6 + m2; v12 = ROTR(v12 ^ v1, 8); v11 += v12; v6 = ROTR(v6 ^ v11, 7);
    v2 = v2 + v7 + m5; v13 = ROTR(v13 ^ v2, 16); v8 += v13; v7 = ROTR(v7 ^ v8, 12); v2 += v7 + m8; v13 = ROTR(v13 ^ v2, 8); v8 += v13; v7 = ROTR(v7 ^ v8, 7);
    v3 = v3 + v4 + m1; v14 = ROTR(v14 ^ v3, 16); v9 += v14; v4 = ROTR(v4 ^ v9, 12); v3 += v4 + m6; v14 = ROTR(v14 ^ v3, 8); v9 += v14; v4 = ROTR(v4 ^ v9, 7);

    // Round 5 with message word permutation.
    v0 = v0 + v4 + m12; v12 = ROTR(v12 ^ v0, 16); v8 = v8 + v12; v4 = ROTR(v4 ^ v8, 12); v0 += v4; v12 = ROTR(v12 ^ v0, 8); v8 += v12; v4 = ROTR(v4 ^ v8, 7);
    v1 = v1 + v5 + m9; v13 = ROTR(v13 ^ v1, 16); v9 = v9 + v13; v5 = ROTR(v5 ^ v9, 12); v1 += v5 + m11; v13 = ROTR(v13 ^ v1, 8); v9 += v13; v5 = ROTR(v5 ^ v9, 7);
    v2 = v2 + v6; v14 = ROTR(v14 ^ v2, 16); v10 += v14; v6 = ROTR(v6 ^ v10, 12); v2 += v6 + m10; v14 = ROTR(v14 ^ v2, 8); v10 += v14; v6 = ROTR(v6 ^ v10, 7);
    v3 = v3 + v7; v15 = ROTR(v15 ^ v3, 16); v11 += v15; v7 = ROTR(v7 ^ v11, 12); v3 += v7 + m8; v15 = ROTR(v15 ^ v3, 8); v11 += v15; v7 = ROTR(v7 ^ v11, 7);
    v0 = v0 + v5 + m7; v15 = ROTR(v15 ^ v0, 16); v10 += v15; v5 = ROTR(v5 ^ v10, 12); v0 += v5 + m2; v15 = ROTR(v15 ^ v0, 8); v10 += v15; v5 = ROTR(v5 ^ v10, 7);
    v1 = v1 + v6 + m5; v12 = ROTR(v12 ^ v1, 16); v11 += v12; v6 = ROTR(v6 ^ v11, 12); v1 += v6 + m3; v12 = ROTR(v12 ^ v1, 8); v11 += v12; v6 = ROTR(v6 ^ v11, 7);
    v2 = v2 + v7 + m0; v13 = ROTR(v13 ^ v2, 16); v8 += v13; v7 = ROTR(v7 ^ v8, 12); v2 += v7 + m1; v13 = ROTR(v13 ^ v2, 8); v8 += v13; v7 = ROTR(v7 ^ v8, 7);
    v3 = v3 + v4 + m6; v14 = ROTR(v14 ^ v3, 16); v9 += v14; v4 = ROTR(v4 ^ v9, 12); v3 += v4 + m4; v14 = ROTR(v14 ^ v3, 8); v9 += v14; v4 = ROTR(v4 ^ v9, 7);

    // Round 6 with message word permutation.
    v0 = v0 + v4 + m9; v12 = ROTR(v12 ^ v0, 16); v8 = v8 + v12; v4 = ROTR(v4 ^ v8, 12); v0 += v4; v12 = ROTR(v12 ^ v0, 8); v8 += v12; v4 = ROTR(v4 ^ v8, 7);
    v1 = v1 + v5 + m11; v13 = ROTR(v13 ^ v1, 16); v9 = v9 + v13; v5 = ROTR(v5 ^ v9, 12); v1 += v5 + m5; v13 = ROTR(v13 ^ v1, 8); v9 += v13; v5 = ROTR(v5 ^ v9, 7);
    v2 = v2 + v6 + m8; v14 = ROTR(v14 ^ v2, 16); v10 += v14; v6 = ROTR(v6 ^ v10, 12); v2 += v6 + m12; v14 = ROTR(v14 ^ v2, 8); v10 += v14; v6 = ROTR(v6 ^ v10, 7);
    v3 = v3 + v7; v15 = ROTR(v15 ^ v3, 16); v11 += v15; v7 = ROTR(v7 ^ v11, 12); v3 += v7 + m1; v15 = ROTR(v15 ^ v3, 8); v11 += v15; v7 = ROTR(v7 ^ v11, 7);
    v0 = v0 + v5; v15 = ROTR(v15 ^ v0, 16); v10 += v15; v5 = ROTR(v5 ^ v10, 12); v0 += v5 + m3; v15 = ROTR(v15 ^ v0, 8); v10 += v15; v5 = ROTR(v5 ^ v10, 7);
    v1 = v1 + v6 + m0; v12 = ROTR(v12 ^ v1, 16); v11 += v12; v6 = ROTR(v6 ^ v11, 12); v1 += v6 + m10; v12 = ROTR(v12 ^ v1, 8); v11 += v12; v6 = ROTR(v6 ^ v11, 7);
    v2 = v2 + v7 + m2; v13 = ROTR(v13 ^ v2, 16); v8 += v13; v7 = ROTR(v7 ^ v8, 12); v2 += v7 + m6; v13 = ROTR(v13 ^ v2, 8); v8 += v13; v7 = ROTR(v7 ^ v8, 7);
    v3 = v3 + v4 + m4; v14 = ROTR(v14 ^ v3, 16); v9 += v14; v4 = ROTR(v4 ^ v9, 12); v3 += v4 + m7; v14 = ROTR(v14 ^ v3, 8); v9 += v14; v4 = ROTR(v4 ^ v9, 7);

    // Round 7 with message word permutation.
    v0 = v0 + v4 + m11; v12 = ROTR(v12 ^ v0, 16); v8 = v8 + v12; v4 = ROTR(v4 ^ v8, 12); v0 += v4; v12 = ROTR(v12 ^ v0, 8); v8 += v12; v4 = ROTR(v4 ^ v8, 7);
    v1 = v1 + v5 + m5; v13 = ROTR(v13 ^ v1, 16); v9 = v9 + v13; v5 = ROTR(v5 ^ v9, 12); v1 += v5 + m0; v13 = ROTR(v13 ^ v1, 8); v9 += v13; v5 = ROTR(v5 ^ v9, 7);
    v2 = v2 + v6 + m1; v14 = ROTR(v14 ^ v2, 16); v10 += v14; v6 = ROTR(v6 ^ v10, 12); v2 += v6 + m9; v14 = ROTR(v14 ^ v2, 8); v10 += v14; v6 = ROTR(v6 ^ v10, 7);
    v3 = v3 + v7 + m8; v15 = ROTR(v15 ^ v3, 16); v11 += v15; v7 = ROTR(v7 ^ v11, 12); v3 += v7 + m6; v15 = ROTR(v15 ^ v3, 8); v11 += v15; v7 = ROTR(v7 ^ v11, 7);
    v0 = v0 + v5; v15 = ROTR(v15 ^ v0, 16); v10 += v15; v5 = ROTR(v5 ^ v10, 12); v0 += v5 + m10; v15 = ROTR(v15 ^ v0, 8); v10 += v15; v5 = ROTR(v5 ^ v10, 7);
    v1 = v1 + v6 + m2; v12 = ROTR(v12 ^ v1, 16); v11 += v12; v6 = ROTR(v6 ^ v11, 12); v1 += v6 + m12; v12 = ROTR(v12 ^ v1, 8); v11 += v12; v6 = ROTR(v6 ^ v11, 7);
    v2 = v2 + v7 + m3; v13 = ROTR(v13 ^ v2, 16); v8 += v13; v7 = ROTR(v7 ^ v8, 12); v2 += v7 + m4; v13 = ROTR(v13 ^ v2, 8); v8 += v13; v7 = ROTR(v7 ^ v8, 7);
    v3 = v3 + v4 + m7; v14 = ROTR(v14 ^ v3, 16); v9 += v14; v4 = ROTR(v4 ^ v9, 12); v3 += v4; v14 = ROTR(v14 ^ v3, 8); v9 += v14; v4 = ROTR(v4 ^ v9, 7);

    // Finally the truncated 256-bit output is defined as:
    //
    // h'0 = v0^v8
    // h'1 = v1^v9
    // h'2 = v2^v10
    // h'3 = v3^v11
    // h'4 = v4^v12
    // h'5 = v5^v13
    // h'6 = v6^v14
    // h'7 = v7^v15
    //
    // Just check if the last word (32-bits) is zeroed and return back to the
    // miner to notify it that a potential solution was found so it can check it
    // against the target difficulty.

    // Debug code to print result of hashing function.
    // if (!((v7 ^ v15) & 0xFFFF0000)) {
    //     printf("hash on gpu %x %x %x %x %x %x %x %x\n",
    //         v0 ^ v8, v1 ^ v9, v2 ^ v10, v3 ^ v11,
    //         v4 ^ v12, v5 ^ v13, v6 ^ v14, v7 ^ v15);
    //     printf("nonce for hash on gpu %x\n", m3);
    // }

    if (v7 ^ v15)
        return;

    // Update nonce.
    uint32_t pos = atomicInc(&output[0], 0xffffffff)+1;
    if (pos > MAX_OUTPUT_RESULTS) return; // Bounds check output buffer.
    output[pos] = m3;
}


extern "C" {
__host__ DLLEXPORT void
decred_blake3_hash(const uint32_t dimgrid, const uint32_t threads, uint32_t *cv, uint32_t *m, uint32_t *out)
{
	search<<<dimgrid, threads>>>(
		out,
		cv[0], cv[1], cv[2], cv[3], cv[4], cv[5], cv[6], cv[7],
		m[0], m[1], m[2],
		// m3,
		m[4], m[5], m[6], m[7], m[8], m[9], m[10], m[11], m[12]

	);
}
}
