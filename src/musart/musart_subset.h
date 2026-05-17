#ifndef __MUSA_HOOK_MUSART_SUBSET_H__
#define __MUSA_HOOK_MUSART_SUBSET_H__

#include <stddef.h>

#ifdef __cplusplus
extern "C" {
#endif

// MUSA Runtime API subset.
//
// MThreads does not ship a public musa_runtime.h equivalent. Symbol names
// and shapes mirror CUDA Runtime API (which MUSA closely clones), cross
// referenced with the public symbol list extracted from libmusart.so in
// MUSA SDK 3.1.0. Only the enumeration / attribute / memory query surface
// is declared here — compute / memcpy / memset APIs are excluded.

enum musaError {
    musaSuccess                  = 0,
    musaErrorInvalidValue        = 1,
    musaErrorMemoryAllocation    = 2,
    musaErrorInitializationError = 3,
    musaErrorNoDevice            = 38,
    musaErrorInvalidDevice       = 101,
    musaErrorNotSupported        = 801,
    musaErrorUnknown             = 999,
};

typedef enum musaError musaError_t;

// musaDeviceProp layout. MUSA Runtime is documented as a CUDA Runtime
// clone, so this struct mirrors cudaDeviceProp byte-for-byte (CUDA 11.x
// layout from src/cudart/cudart_subset.h). Real callers link against
// MThreads' musa_runtime.h which is expected to carry the same shape;
// musaGetDeviceProperties writes into this buffer so the layout must
// match what the caller's compiler sized.
//
// musaUUID — 16-byte UUID, mirrors cudaUUID_t.
typedef struct musaUUID {
    char bytes[16];
} musaUUID_t;

struct musaDeviceProp {
    char         name[256];
    musaUUID_t   uuid;
    char         luid[8];
    unsigned int luidDeviceNodeMask;
    size_t       totalGlobalMem;
    size_t       sharedMemPerBlock;
    int          regsPerBlock;
    int          warpSize;
    size_t       memPitch;
    int          maxThreadsPerBlock;
    int          maxThreadsDim[3];
    int          maxGridSize[3];
    int          clockRate;
    size_t       totalConstMem;
    int          major;
    int          minor;
    size_t       textureAlignment;
    size_t       texturePitchAlignment;
    int          deviceOverlap;
    int          multiProcessorCount;
    int          kernelExecTimeoutEnabled;
    int          integrated;
    int          canMapHostMemory;
    int          computeMode;
    int          maxTexture1D;
    int          maxTexture1DMipmap;
    int          maxTexture1DLinear;
    int          maxTexture2D[2];
    int          maxTexture2DMipmap[2];
    int          maxTexture2DLinear[3];
    int          maxTexture2DGather[2];
    int          maxTexture3D[3];
    int          maxTexture3DAlt[3];
    int          maxTextureCubemap;
    int          maxTexture1DLayered[2];
    int          maxTexture2DLayered[3];
    int          maxTextureCubemapLayered[2];
    int          maxSurface1D;
    int          maxSurface2D[2];
    int          maxSurface3D[3];
    int          maxSurface1DLayered[2];
    int          maxSurface2DLayered[3];
    int          maxSurfaceCubemap;
    int          maxSurfaceCubemapLayered[2];
    size_t       surfaceAlignment;
    int          concurrentKernels;
    int          ECCEnabled;
    int          pciBusID;
    int          pciDeviceID;
    int          pciDomainID;
    int          tccDriver;
    int          asyncEngineCount;
    int          unifiedAddressing;
    int          memoryClockRate;
    int          memoryBusWidth;
    int          l2CacheSize;
    int          persistingL2CacheMaxSize;
    int          maxThreadsPerMultiProcessor;
    int          streamPrioritiesSupported;
    int          globalL1CacheSupported;
    int          localL1CacheSupported;
    size_t       sharedMemPerMultiprocessor;
    int          regsPerMultiprocessor;
    int          managedMemory;
    int          isMultiGpuBoard;
    int          multiGpuBoardGroupID;
    int          hostNativeAtomicSupported;
    int          singleToDoublePrecisionPerfRatio;
    int          pageableMemoryAccess;
    int          concurrentManagedAccess;
    int          computePreemptionSupported;
    int          canUseHostPointerForRegisteredMem;
    int          cooperativeLaunch;
    int          cooperativeMultiDeviceLaunch;
    size_t       sharedMemPerBlockOptin;
    int          pageableMemoryAccessUsesHostPageTables;
    int          directManagedMemAccessFromHost;
    int          maxBlocksPerMultiProcessor;
    int          accessPolicyMaxWindowSize;
    size_t       reservedSharedMemPerBlock;
};

#ifdef __cplusplus
}
#endif

#endif  // __MUSA_HOOK_MUSART_SUBSET_H__
