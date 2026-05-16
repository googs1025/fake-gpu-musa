// Package mtml is a thin cgo wrapper around libmtml.so. It only covers
// what mthreads-gmi needs; expand on demand.
package mtml

/*
#cgo LDFLAGS: -ldl
#include <dlfcn.h>
#include <stdlib.h>
#include <string.h>

typedef int MtmlReturn;
typedef struct MtmlLibrary MtmlLibrary;
typedef struct MtmlSystem  MtmlSystem;
typedef struct MtmlDevice  MtmlDevice;
typedef struct MtmlMemory  MtmlMemory;
typedef struct MtmlGpu     MtmlGpu;

typedef MtmlReturn (*lib_init_fn)(MtmlLibrary**);
typedef MtmlReturn (*lib_shutdown_fn)(MtmlLibrary*);
typedef MtmlReturn (*lib_count_fn)(const MtmlLibrary*, unsigned int*);
typedef MtmlReturn (*lib_init_dev_fn)(const MtmlLibrary*, unsigned int, MtmlDevice**);
typedef MtmlReturn (*lib_free_dev_fn)(MtmlDevice*);
typedef MtmlReturn (*dev_name_fn)(const MtmlDevice*, char*, unsigned int);
typedef MtmlReturn (*dev_uuid_fn)(const MtmlDevice*, char*, unsigned int);
typedef MtmlReturn (*dev_pwr_fn)(const MtmlDevice*, unsigned int*);
typedef MtmlReturn (*dev_init_mem_fn)(const MtmlDevice*, MtmlMemory**);
typedef MtmlReturn (*dev_free_mem_fn)(MtmlMemory*);
typedef MtmlReturn (*mem_total_fn)(const MtmlMemory*, unsigned long long*);
typedef MtmlReturn (*mem_used_fn)(const MtmlMemory*, unsigned long long*);
typedef MtmlReturn (*dev_init_gpu_fn)(const MtmlDevice*, MtmlGpu**);
typedef MtmlReturn (*dev_free_gpu_fn)(MtmlGpu*);
typedef MtmlReturn (*gpu_util_fn)(const MtmlGpu*, unsigned int*);
typedef MtmlReturn (*gpu_temp_fn)(const MtmlGpu*, int*);
typedef MtmlReturn (*sys_drv_fn)(const MtmlSystem*, char*, unsigned int);
typedef MtmlReturn (*lib_init_sys_fn)(const MtmlLibrary*, MtmlSystem**);
typedef MtmlReturn (*lib_free_sys_fn)(MtmlSystem*);

static void *h = NULL;

static lib_init_fn      mtmlLibraryInit_fn;
static lib_shutdown_fn  mtmlLibraryShutDown_fn;
static lib_count_fn     mtmlLibraryCountDevice_fn;
static lib_init_dev_fn  mtmlLibraryInitDeviceByIndex_fn;
static lib_free_dev_fn  mtmlLibraryFreeDevice_fn;
static dev_name_fn      mtmlDeviceGetName_fn;
static dev_uuid_fn      mtmlDeviceGetUUID_fn;
static dev_pwr_fn       mtmlDeviceGetPowerUsage_fn;
static dev_init_mem_fn  mtmlDeviceInitMemory_fn;
static dev_free_mem_fn  mtmlDeviceFreeMemory_fn;
static mem_total_fn     mtmlMemoryGetTotal_fn;
static mem_used_fn      mtmlMemoryGetUsed_fn;
static dev_init_gpu_fn  mtmlDeviceInitGpu_fn;
static dev_free_gpu_fn  mtmlDeviceFreeGpu_fn;
static gpu_util_fn      mtmlGpuGetUtilization_fn;
static gpu_temp_fn      mtmlGpuGetTemperature_fn;
static sys_drv_fn       mtmlSystemGetDriverVersion_fn;
static lib_init_sys_fn  mtmlLibraryInitSystem_fn;
static lib_free_sys_fn  mtmlLibraryFreeSystem_fn;

#define LOAD(sym, type) sym##_fn = (type)dlsym(h, #sym)

static int load_lib(const char *path) {
    h = dlopen(path, RTLD_NOW | RTLD_GLOBAL);
    if (!h) return -1;
    LOAD(mtmlLibraryInit,                lib_init_fn);
    LOAD(mtmlLibraryShutDown,            lib_shutdown_fn);
    LOAD(mtmlLibraryCountDevice,         lib_count_fn);
    LOAD(mtmlLibraryInitDeviceByIndex,   lib_init_dev_fn);
    LOAD(mtmlLibraryFreeDevice,          lib_free_dev_fn);
    LOAD(mtmlDeviceGetName,              dev_name_fn);
    LOAD(mtmlDeviceGetUUID,              dev_uuid_fn);
    LOAD(mtmlDeviceGetPowerUsage,        dev_pwr_fn);
    LOAD(mtmlDeviceInitMemory,           dev_init_mem_fn);
    LOAD(mtmlDeviceFreeMemory,           dev_free_mem_fn);
    LOAD(mtmlMemoryGetTotal,             mem_total_fn);
    LOAD(mtmlMemoryGetUsed,              mem_used_fn);
    LOAD(mtmlDeviceInitGpu,              dev_init_gpu_fn);
    LOAD(mtmlDeviceFreeGpu,              dev_free_gpu_fn);
    LOAD(mtmlGpuGetUtilization,          gpu_util_fn);
    LOAD(mtmlGpuGetTemperature,          gpu_temp_fn);
    LOAD(mtmlSystemGetDriverVersion,     sys_drv_fn);
    LOAD(mtmlLibraryInitSystem,          lib_init_sys_fn);
    LOAD(mtmlLibraryFreeSystem,          lib_free_sys_fn);
    return 0;
}

// Trampolines so Go can call function pointers indirectly.
static MtmlReturn lib_init(MtmlLibrary **l)                                       { return mtmlLibraryInit_fn(l); }
static MtmlReturn lib_shutdown(MtmlLibrary *l)                                    { return mtmlLibraryShutDown_fn(l); }
static MtmlReturn lib_count(const MtmlLibrary *l, unsigned int *c)                { return mtmlLibraryCountDevice_fn(l, c); }
static MtmlReturn lib_init_dev(const MtmlLibrary *l, unsigned int i, MtmlDevice **d) { return mtmlLibraryInitDeviceByIndex_fn(l, i, d); }
static MtmlReturn lib_free_dev(MtmlDevice *d)                                     { return mtmlLibraryFreeDevice_fn(d); }
static MtmlReturn dev_name(const MtmlDevice *d, char *b, unsigned int n)          { return mtmlDeviceGetName_fn(d, b, n); }
static MtmlReturn dev_uuid(const MtmlDevice *d, char *b, unsigned int n)          { return mtmlDeviceGetUUID_fn(d, b, n); }
static MtmlReturn dev_pwr(const MtmlDevice *d, unsigned int *p)                   { return mtmlDeviceGetPowerUsage_fn(d, p); }
static MtmlReturn dev_init_mem(const MtmlDevice *d, MtmlMemory **m)               { return mtmlDeviceInitMemory_fn(d, m); }
static MtmlReturn dev_free_mem(MtmlMemory *m)                                     { return mtmlDeviceFreeMemory_fn(m); }
static MtmlReturn mem_total(const MtmlMemory *m, unsigned long long *v)           { return mtmlMemoryGetTotal_fn(m, v); }
static MtmlReturn mem_used(const MtmlMemory *m, unsigned long long *v)            { return mtmlMemoryGetUsed_fn(m, v); }
static MtmlReturn dev_init_gpu(const MtmlDevice *d, MtmlGpu **g)                  { return mtmlDeviceInitGpu_fn(d, g); }
static MtmlReturn dev_free_gpu(MtmlGpu *g)                                        { return mtmlDeviceFreeGpu_fn(g); }
static MtmlReturn gpu_util(const MtmlGpu *g, unsigned int *v)                     { return mtmlGpuGetUtilization_fn(g, v); }
static MtmlReturn gpu_temp(const MtmlGpu *g, int *v)                              { return mtmlGpuGetTemperature_fn(g, v); }
static MtmlReturn sys_drv(const MtmlSystem *s, char *b, unsigned int n)           { return mtmlSystemGetDriverVersion_fn(s, b, n); }
static MtmlReturn lib_init_sys(const MtmlLibrary *l, MtmlSystem **s)              { return mtmlLibraryInitSystem_fn(l, s); }
static MtmlReturn lib_free_sys(MtmlSystem *s)                                     { return mtmlLibraryFreeSystem_fn(s); }
*/
import "C"

import (
	"errors"
	"fmt"
	"unsafe"
)

const SUCCESS = 0

type Library struct{ ptr unsafe.Pointer }
type System struct{ ptr unsafe.Pointer }
type Device struct{ ptr unsafe.Pointer }
type Memory struct{ ptr unsafe.Pointer }
type GPU struct{ ptr unsafe.Pointer }

// Load opens libmtml.so and resolves symbols. Path is the file name or
// absolute path passed straight to dlopen, e.g. "libmtml.so" relies on
// the dynamic loader's search rules.
func Load(path string) error {
	cpath := C.CString(path)
	defer C.free(unsafe.Pointer(cpath))
	if C.load_lib(cpath) != 0 {
		return errors.New("mtml: dlopen failed: " + path)
	}
	return nil
}

func Init() (*Library, error) {
	var l unsafe.Pointer
	if r := C.lib_init((**C.MtmlLibrary)(unsafe.Pointer(&l))); r != SUCCESS {
		return nil, fmt.Errorf("mtmlLibraryInit failed: %d", int(r))
	}
	return &Library{ptr: l}, nil
}

func (l *Library) Shutdown() error {
	if r := C.lib_shutdown((*C.MtmlLibrary)(l.ptr)); r != SUCCESS {
		return fmt.Errorf("mtmlLibraryShutDown: %d", int(r))
	}
	return nil
}

func (l *Library) DeviceCount() (uint, error) {
	var n C.uint
	if r := C.lib_count((*C.MtmlLibrary)(l.ptr), &n); r != SUCCESS {
		return 0, fmt.Errorf("mtmlLibraryCountDevice: %d", int(r))
	}
	return uint(n), nil
}

func (l *Library) Device(i uint) (*Device, error) {
	var d unsafe.Pointer
	if r := C.lib_init_dev((*C.MtmlLibrary)(l.ptr), C.uint(i), (**C.MtmlDevice)(unsafe.Pointer(&d))); r != SUCCESS {
		return nil, fmt.Errorf("mtmlLibraryInitDeviceByIndex(%d): %d", i, int(r))
	}
	return &Device{ptr: d}, nil
}

func (d *Device) Free() error {
	if r := C.lib_free_dev((*C.MtmlDevice)(d.ptr)); r != SUCCESS {
		return fmt.Errorf("mtmlLibraryFreeDevice: %d", int(r))
	}
	return nil
}

func (l *Library) System() (*System, error) {
	var s unsafe.Pointer
	if r := C.lib_init_sys((*C.MtmlLibrary)(l.ptr), (**C.MtmlSystem)(unsafe.Pointer(&s))); r != SUCCESS {
		return nil, fmt.Errorf("mtmlLibraryInitSystem: %d", int(r))
	}
	return &System{ptr: s}, nil
}

func (s *System) Free() error {
	if r := C.lib_free_sys((*C.MtmlSystem)(s.ptr)); r != SUCCESS {
		return fmt.Errorf("mtmlLibraryFreeSystem: %d", int(r))
	}
	return nil
}

func (s *System) DriverVersion() (string, error) {
	buf := make([]byte, 80)
	if r := C.sys_drv((*C.MtmlSystem)(s.ptr), (*C.char)(unsafe.Pointer(&buf[0])), 80); r != SUCCESS {
		return "", fmt.Errorf("mtmlSystemGetDriverVersion: %d", int(r))
	}
	return C.GoString((*C.char)(unsafe.Pointer(&buf[0]))), nil
}

func (d *Device) Name() (string, error) {
	buf := make([]byte, 64)
	if r := C.dev_name((*C.MtmlDevice)(d.ptr), (*C.char)(unsafe.Pointer(&buf[0])), 64); r != SUCCESS {
		return "", fmt.Errorf("mtmlDeviceGetName: %d", int(r))
	}
	return C.GoString((*C.char)(unsafe.Pointer(&buf[0]))), nil
}

func (d *Device) UUID() (string, error) {
	buf := make([]byte, 64)
	if r := C.dev_uuid((*C.MtmlDevice)(d.ptr), (*C.char)(unsafe.Pointer(&buf[0])), 64); r != SUCCESS {
		return "", fmt.Errorf("mtmlDeviceGetUUID: %d", int(r))
	}
	return C.GoString((*C.char)(unsafe.Pointer(&buf[0]))), nil
}

// PowerUsage returns the current power draw in milliwatts.
func (d *Device) PowerUsage() (uint, error) {
	var p C.uint
	if r := C.dev_pwr((*C.MtmlDevice)(d.ptr), &p); r != SUCCESS {
		return 0, fmt.Errorf("mtmlDeviceGetPowerUsage: %d", int(r))
	}
	return uint(p), nil
}

// Memory pulls total/used bytes and frees the sub-object handle on the way out.
func (d *Device) Memory() (total, used uint64, err error) {
	var m unsafe.Pointer
	if r := C.dev_init_mem((*C.MtmlDevice)(d.ptr), (**C.MtmlMemory)(unsafe.Pointer(&m))); r != SUCCESS {
		return 0, 0, fmt.Errorf("mtmlDeviceInitMemory: %d", int(r))
	}
	defer C.dev_free_mem((*C.MtmlMemory)(m))
	var t, u C.ulonglong
	if r := C.mem_total((*C.MtmlMemory)(m), &t); r != SUCCESS {
		return 0, 0, fmt.Errorf("mtmlMemoryGetTotal: %d", int(r))
	}
	if r := C.mem_used((*C.MtmlMemory)(m), &u); r != SUCCESS {
		return 0, 0, fmt.Errorf("mtmlMemoryGetUsed: %d", int(r))
	}
	return uint64(t), uint64(u), nil
}

// GPUStats bundles GPU sub-object queries (util + temp) behind one
// Init/Free pair so callers do not see the handle dance.
func (d *Device) GPUStats() (util uint, tempC int, err error) {
	var g unsafe.Pointer
	if r := C.dev_init_gpu((*C.MtmlDevice)(d.ptr), (**C.MtmlGpu)(unsafe.Pointer(&g))); r != SUCCESS {
		return 0, 0, fmt.Errorf("mtmlDeviceInitGpu: %d", int(r))
	}
	defer C.dev_free_gpu((*C.MtmlGpu)(g))
	var pct C.uint
	if r := C.gpu_util((*C.MtmlGpu)(g), &pct); r != SUCCESS {
		return 0, 0, fmt.Errorf("mtmlGpuGetUtilization: %d", int(r))
	}
	var t C.int
	if r := C.gpu_temp((*C.MtmlGpu)(g), &t); r != SUCCESS {
		return 0, 0, fmt.Errorf("mtmlGpuGetTemperature: %d", int(r))
	}
	return uint(pct), int(t), nil
}
