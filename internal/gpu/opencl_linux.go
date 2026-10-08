//go:build linux && cgo

package gpu

/*
#cgo LDFLAGS: -ldl
#include <dlfcn.h>
#include <stdint.h>
#include <stdlib.h>
#include <stdio.h>
#include <string.h>

typedef int32_t cl_int;
typedef uint32_t cl_uint;
typedef uint64_t cl_ulong;
typedef uint64_t cl_bitfield;
typedef cl_bitfield cl_device_type;
typedef intptr_t cl_context_properties;
typedef intptr_t cl_queue_properties;
typedef uint32_t cl_bool;
typedef uint32_t cl_device_info;
typedef uint32_t cl_program_build_info;
typedef uint64_t cl_mem_flags;
typedef uint32_t cl_kernel_work_group_info;
typedef void *cl_platform_id;
typedef void *cl_device_id;
typedef void *cl_context;
typedef void *cl_command_queue;
typedef void *cl_program;
typedef void *cl_kernel;
typedef void *cl_mem;
typedef void (*cl_context_callback)(const char *, const void *, size_t, void *);

#define CL_DEVICE_TYPE_GPU (1ULL << 2)
#define CL_DEVICE_NAME 0x102B
#define CL_DEVICE_VENDOR 0x102C
#define CL_DEVICE_VENDOR_ID 0x1001
#define CL_DEVICE_MAX_COMPUTE_UNITS 0x1002
#define CL_DEVICE_MAX_WORK_GROUP_SIZE 0x1004
#define CL_MEM_WRITE_ONLY (1ULL << 1)
#define CL_PROGRAM_BUILD_LOG 0x1183
#define CL_KERNEL_WORK_GROUP_SIZE 0x11B0

typedef cl_int (*p_clGetPlatformIDs)(cl_uint, cl_platform_id *, cl_uint *);
typedef cl_int (*p_clGetDeviceIDs)(cl_platform_id, cl_device_type, cl_uint, cl_device_id *, cl_uint *);
typedef cl_int (*p_clGetDeviceInfo)(cl_device_id, cl_device_info, size_t, void *, size_t *);
typedef cl_context (*p_clCreateContext)(const cl_context_properties *, cl_uint, const cl_device_id *, cl_context_callback, void *, cl_int *);
typedef cl_command_queue (*p_clCreateCommandQueue)(cl_context, cl_device_id, cl_bitfield, cl_int *);
typedef cl_program (*p_clCreateProgramWithSource)(cl_context, cl_uint, const char **, const size_t *, cl_int *);
typedef cl_int (*p_clBuildProgram)(cl_program, cl_uint, const cl_device_id *, const char *, void (*)(cl_program, void *), void *);
typedef cl_int (*p_clGetProgramBuildInfo)(cl_program, cl_device_id, cl_program_build_info, size_t, void *, size_t *);
typedef cl_kernel (*p_clCreateKernel)(cl_program, const char *, cl_int *);
typedef cl_int (*p_clSetKernelArg)(cl_kernel, cl_uint, size_t, const void *);
typedef cl_mem (*p_clCreateBuffer)(cl_context, cl_mem_flags, size_t, void *, cl_int *);
typedef cl_int (*p_clEnqueueNDRangeKernel)(cl_command_queue, cl_kernel, cl_uint, const size_t *, const size_t *, const size_t *, cl_uint, const void *, void *);
typedef cl_int (*p_clFinish)(cl_command_queue);
typedef cl_int (*p_clEnqueueReadBuffer)(cl_command_queue, cl_mem, cl_bool, size_t, size_t, void *, cl_uint, const void *, void *);
typedef cl_int (*p_clReleaseMemObject)(cl_mem);
typedef cl_int (*p_clReleaseKernel)(cl_kernel);
typedef cl_int (*p_clReleaseProgram)(cl_program);
typedef cl_int (*p_clReleaseCommandQueue)(cl_command_queue);
typedef cl_int (*p_clReleaseContext)(cl_context);

static void *omls_cl_lib;
static p_clGetPlatformIDs p_clGetPlatformIDs_fn;
static p_clGetDeviceIDs p_clGetDeviceIDs_fn;
static p_clGetDeviceInfo p_clGetDeviceInfo_fn;
static p_clCreateContext p_clCreateContext_fn;
static p_clCreateCommandQueue p_clCreateCommandQueue_fn;
static p_clCreateProgramWithSource p_clCreateProgramWithSource_fn;
static p_clBuildProgram p_clBuildProgram_fn;
static p_clGetProgramBuildInfo p_clGetProgramBuildInfo_fn;
static p_clCreateKernel p_clCreateKernel_fn;
static p_clSetKernelArg p_clSetKernelArg_fn;
static p_clCreateBuffer p_clCreateBuffer_fn;
static p_clEnqueueNDRangeKernel p_clEnqueueNDRangeKernel_fn;
static p_clFinish p_clFinish_fn;
static p_clEnqueueReadBuffer p_clEnqueueReadBuffer_fn;
static p_clReleaseMemObject p_clReleaseMemObject_fn;
static p_clReleaseKernel p_clReleaseKernel_fn;
static p_clReleaseProgram p_clReleaseProgram_fn;
static p_clReleaseCommandQueue p_clReleaseCommandQueue_fn;
static p_clReleaseContext p_clReleaseContext_fn;

#define LOAD(name) if (!p_##name##_fn) p_##name##_fn = (p_##name)dlsym(omls_cl_lib, #name); if (!p_##name##_fn) return -1000
#define clGetPlatformIDs p_clGetPlatformIDs_fn
#define clGetDeviceIDs p_clGetDeviceIDs_fn
#define clGetDeviceInfo p_clGetDeviceInfo_fn
#define clCreateContext p_clCreateContext_fn
#define clCreateCommandQueue p_clCreateCommandQueue_fn
#define clCreateProgramWithSource p_clCreateProgramWithSource_fn
#define clBuildProgram p_clBuildProgram_fn
#define clGetProgramBuildInfo p_clGetProgramBuildInfo_fn
#define clCreateKernel p_clCreateKernel_fn
#define clSetKernelArg p_clSetKernelArg_fn
#define clCreateBuffer p_clCreateBuffer_fn
#define clEnqueueNDRangeKernel p_clEnqueueNDRangeKernel_fn
#define clFinish p_clFinish_fn
#define clEnqueueReadBuffer p_clEnqueueReadBuffer_fn
#define clReleaseMemObject p_clReleaseMemObject_fn
#define clReleaseKernel p_clReleaseKernel_fn
#define clReleaseProgram p_clReleaseProgram_fn
#define clReleaseCommandQueue p_clReleaseCommandQueue_fn
#define clReleaseContext p_clReleaseContext_fn
static int omls_cl_init(void) {
  if (!omls_cl_lib) omls_cl_lib = dlopen("libOpenCL.so.1", RTLD_NOW | RTLD_LOCAL);
  if (!omls_cl_lib) return -1000;
  LOAD(clGetPlatformIDs); LOAD(clGetDeviceIDs); LOAD(clGetDeviceInfo);
  return 0;
}
static int omls_cl_devices(cl_device_id *out, int max) {
  if (omls_cl_init()) return -1000;
  cl_uint np = 0;
  cl_int err = clGetPlatformIDs(0, NULL, &np);
  if (err || !np) return 0;
  cl_platform_id *ps = (cl_platform_id*)calloc(np, sizeof(*ps));
  if (!ps) return -1001;
  err = clGetPlatformIDs(np, ps, NULL);
  int total = 0;
  if (!err) for (cl_uint p = 0; p < np; p++) {
    cl_uint nd = 0;
    err = clGetDeviceIDs(ps[p], CL_DEVICE_TYPE_GPU, 0, NULL, &nd);
    if (err || !nd) continue;
    cl_device_id *ds = (cl_device_id*)calloc(nd, sizeof(*ds));
    if (!ds) continue;
    if (!clGetDeviceIDs(ps[p], CL_DEVICE_TYPE_GPU, nd, ds, NULL)) {
      for (cl_uint d = 0; d < nd; d++) {
        if (total < max) out[total] = ds[d];
        total++;
      }
    }
    free(ds);
  }
  free(ps);
  return total;
}
static int omls_cl_device_info(int idx, char *name, size_t name_cap, char *vendor, size_t vendor_cap, cl_uint *vendor_id, cl_uint *compute_units) {
  if (omls_cl_init()) return -1000;
  cl_device_id ds[128]; int n = omls_cl_devices(ds, 128);
  if (idx < 0 || idx >= n || idx >= 128) return -1002;
  size_t got = 0;
  cl_int e = clGetDeviceInfo(ds[idx], CL_DEVICE_NAME, name_cap, name, &got); if (e) return e;
  e = clGetDeviceInfo(ds[idx], CL_DEVICE_VENDOR, vendor_cap, vendor, &got); if (e) return e;
  e = clGetDeviceInfo(ds[idx], CL_DEVICE_VENDOR_ID, sizeof(*vendor_id), vendor_id, NULL); if (e) return e;
  e = clGetDeviceInfo(ds[idx], CL_DEVICE_MAX_COMPUTE_UNITS, sizeof(*compute_units), compute_units, NULL); return e;
}
static int omls_cl_burn(int idx, cl_ulong iterations, char *error, size_t error_cap) {
  if (omls_cl_init()) { snprintf(error, error_cap, "OpenCL loader unavailable"); return -1000; }
  LOAD(clCreateContext); LOAD(clCreateCommandQueue); LOAD(clCreateProgramWithSource); LOAD(clBuildProgram);
  LOAD(clGetProgramBuildInfo); LOAD(clCreateKernel); LOAD(clSetKernelArg); LOAD(clCreateBuffer);
  LOAD(clEnqueueNDRangeKernel); LOAD(clFinish); LOAD(clEnqueueReadBuffer); LOAD(clReleaseMemObject);
  LOAD(clReleaseKernel); LOAD(clReleaseProgram); LOAD(clReleaseCommandQueue); LOAD(clReleaseContext);
  cl_device_id ds[128]; int n = omls_cl_devices(ds, 128);
  if (idx < 0 || idx >= n || idx >= 128) { snprintf(error, error_cap, "OpenCL GPU index %d unavailable (found %d)", idx, n); return -1002; }
  cl_device_id dev = ds[idx]; cl_int e = 0;
  cl_context ctx = clCreateContext(NULL, 1, &dev, NULL, NULL, &e); if (!ctx || e) goto fail;
  cl_command_queue q = clCreateCommandQueue(ctx, dev, 0, &e); if (!q || e) goto fail_ctx;
  const char *src = "__kernel void omls_burn(__global ulong *out, ulong rounds) { size_t i=get_global_id(0); ulong x=(ulong)i+1; for(ulong j=0;j<rounds;j++){ x=x*1664525UL+1013904223UL; x^=x<<13; x^=x>>7; x^=x<<17; } out[i]=x; }";
  cl_program prog = clCreateProgramWithSource(ctx, 1, &src, NULL, &e); if (!prog || e) goto fail_q;
  e = clBuildProgram(prog, 1, &dev, "", NULL, NULL);
  if (e) { size_t z=0; clGetProgramBuildInfo(prog, dev, CL_PROGRAM_BUILD_LOG, 0, NULL, &z); if (z && z < error_cap) clGetProgramBuildInfo(prog, dev, CL_PROGRAM_BUILD_LOG, z, error, NULL); goto fail_prog; }
  cl_kernel kernel = clCreateKernel(prog, "omls_burn", &e); if (!kernel || e) goto fail_prog;
  size_t global = 4096; cl_uint units = 1; clGetDeviceInfo(dev, CL_DEVICE_MAX_COMPUTE_UNITS, sizeof(units), &units, NULL); if (units > 0) global = (size_t)units * 256;
  if (global > 65536) global = 65536;
  cl_ulong rounds = iterations / global; if (rounds < 1) rounds = 1;
  cl_mem out = clCreateBuffer(ctx, CL_MEM_WRITE_ONLY, global * sizeof(cl_ulong), NULL, &e); if (!out || e) goto fail_kernel;
  e = clSetKernelArg(kernel, 0, sizeof(out), &out); if (e) goto fail_mem;
  e = clSetKernelArg(kernel, 1, sizeof(rounds), &rounds); if (e) goto fail_mem;
  e = clEnqueueNDRangeKernel(q, kernel, 1, NULL, &global, NULL, 0, NULL, NULL); if (e) goto fail_mem;
  e = clFinish(q); if (e) goto fail_mem;
  cl_ulong *results = (cl_ulong*)calloc(global, sizeof(cl_ulong)); if (!results) { e=-1001; goto fail_mem; }
  e = clEnqueueReadBuffer(q, out, 1, 0, global*sizeof(cl_ulong), results, 0, NULL, NULL); free(results);
  clReleaseMemObject(out); clReleaseKernel(kernel); clReleaseProgram(prog); clReleaseCommandQueue(q); clReleaseContext(ctx); return e;
fail_mem: clReleaseMemObject(out);
fail_kernel: clReleaseKernel(kernel);
fail_prog: clReleaseProgram(prog);
fail_q: clReleaseCommandQueue(q);
fail_ctx: clReleaseContext(ctx);
fail: if (!error[0]) snprintf(error, error_cap, "OpenCL error %d", e); return e ? e : -1003;
}
*/
import "C"

import (
	"fmt"
	"strings"
	"time"
)

type Device struct {
	Index       int
	Name        string
	Vendor      string
	VendorID    uint32
	ComputeUnit uint32
}

func Devices() ([]Device, error) {
	var raw [128]C.cl_device_id
	n := int(C.omls_cl_devices(&raw[0], C.int(len(raw))))
	if n < 0 {
		return nil, fmt.Errorf("OpenCL unavailable")
	}
	if n > len(raw) {
		n = len(raw)
	}
	devices := make([]Device, 0, n)
	for i := 0; i < n; i++ {
		var name, vendor [512]C.char
		var vendorID, units C.cl_uint
		if rc := C.omls_cl_device_info(C.int(i), &name[0], C.size_t(len(name)), &vendor[0], C.size_t(len(vendor)), &vendorID, &units); rc != 0 {
			return nil, fmt.Errorf("query OpenCL GPU %d failed (%d)", i, int(rc))
		}
		devices = append(devices, Device{Index: i, Name: C.GoString(&name[0]), Vendor: C.GoString(&vendor[0]), VendorID: uint32(vendorID), ComputeUnit: uint32(units)})
	}
	return devices, nil
}

func Burn(index int, iterations int64) (time.Duration, error) {
	if iterations < 1 {
		iterations = 1
	}
	start := time.Now()
	var errText [2048]C.char
	rc := C.omls_cl_burn(C.int(index), C.cl_ulong(iterations), &errText[0], C.size_t(len(errText)))
	if rc != 0 {
		msg := strings.TrimSpace(C.GoString(&errText[0]))
		if msg == "" {
			msg = fmt.Sprintf("OpenCL GPU execution failed (%d)", int(rc))
		}
		return time.Since(start), fmt.Errorf("%s", msg)
	}
	return time.Since(start), nil
}
