//go:build darwin && cgo

package monitor

/*
#include <stdint.h>
#include <mach/mach.h>
#include <mach/task_info.h>

// phys_footprint 是 macOS 进程内存足迹，也是 Activity Monitor 进程 Memory
// 列表所采用的主要系统口径。旧系统不支持 TASK_VM_INFO 时，退回 resident_size。
static int read_current_task_memory(uint64_t *footprint,
                                    uint64_t *resident,
                                    uint64_t *compressed,
                                    int *used_footprint) {
    task_vm_info_data_t info = {0};
    mach_msg_type_number_t count = TASK_VM_INFO_REV1_COUNT;
    kern_return_t kr = task_info(mach_task_self(), TASK_VM_INFO,
                                 (task_info_t)&info, &count);
    if (kr == KERN_SUCCESS) {
        *footprint = (uint64_t)info.phys_footprint;
        *resident = (uint64_t)info.resident_size;
        *compressed = (uint64_t)info.compressed;
        *used_footprint = 1;
        return 0;
    }

    mach_task_basic_info_data_t basic = {0};
    count = MACH_TASK_BASIC_INFO_COUNT;
    kr = task_info(mach_task_self(), MACH_TASK_BASIC_INFO,
                   (task_info_t)&basic, &count);
    if (kr == KERN_SUCCESS) {
        *footprint = (uint64_t)basic.resident_size;
        *resident = (uint64_t)basic.resident_size;
        *compressed = 0;
        *used_footprint = 0;
        return 0;
    }

    return (int)kr;
}
*/
import "C"

import "fmt"

// readProcessMemoryStats 使用 Mach TASK_VM_INFO 读取当前进程的物理内存足迹。
func readProcessMemoryStats() (processMemoryStats, error) {
	var stats processMemoryStats
	var footprint, resident, compressed C.uint64_t
	var usedFootprint C.int

	if code := C.read_current_task_memory(&footprint, &resident, &compressed, &usedFootprint); code != 0 {
		return stats, fmt.Errorf("task_info 读取进程内存失败，kern_return=%d", int(code))
	}

	stats.AppMemoryBytes = uint64(footprint)
	stats.ResidentBytes = uint64(resident)
	stats.HasAppMemory = true
	stats.HasResident = true
	stats.CompressedBytes = uint64(compressed)
	stats.HasCompressed = true
	if usedFootprint != 0 {
		stats.AppMemoryMetric = "PhysicalFootprint"
		stats.AppMemoryLabel = "物理足迹"
	} else {
		stats.AppMemoryMetric = "ResidentSizeFallback"
		stats.AppMemoryLabel = "总驻留内存（兼容回退）"
	}
	return stats, nil
}
