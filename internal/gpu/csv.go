package gpu

const WhatapGPUMetricsCSV = `
    # Static configuration information
    DCGM_FI_DRIVER_VERSION, label, Driver Version.                           # code 1
    DCGM_FI_NVML_VERSION,   label, NVML Version.                               # code 2
    DCGM_FI_DEV_NAME,       label, Device Name.                               # code 50
    DCGM_FI_DEV_SERIAL,     label, Device Serial Number.                      # code 53
    DCGM_FI_DEV_UUID,       label, Device UUID.                               # code 54
    DCGM_FI_DEV_COMPUTE_MODE,      label, Compute mode of the device.          # code 65
    DCGM_FI_DEV_PERSISTENCE_MODE,  label, Persistence mode status.           # code 66
    DCGM_FI_DEV_VIRTUAL_MODE,      label, Virtual mode status               # code 500
    DCGM_FI_DEV_MIG_MODE,          label, MIG mode status.                     # code 67
    DCGM_FI_DEV_MIG_MAX_SLICES,    label, Maximum MIG slices available.          # code 69
    DCGM_FI_DEV_MIG_GI_INFO,       label, MIG Graphics Instance information.   # code 76
    DCGM_FI_DEV_MIG_CI_INFO,       label, MIG Compute Instance information.    # code 77

    # Clocks
    DCGM_FI_DEV_SM_CLOCK,  gauge, SM clock frequency (in MHz).                   # code 100
    DCGM_FI_DEV_MEM_CLOCK, gauge, Memory clock frequency (in MHz).                 # code 101
    #DCGM_FI_DEV_APP_SM_CLOCK, gauge, Application SM clock frequency (in MHz).        # code 110
    #DCGM_FI_DEV_APP_MEM_CLOCK, gauge, Application Memory clock frequency (in MHz).     # code 111
    #DCGM_FI_DEV_VIDEO_CLOCK, gauge, Video clock frequency (in MHz).                  # code 102

    # Power
    #DCGM_FI_DEV_ENFORCED_POWER_LIMIT, gauge, Enforced power limit (in W).            # code 164
    DCGM_FI_DEV_POWER_USAGE,              gauge, Power usage (in W).                # code 155

    # Performance state & Fan
    DCGM_FI_DEV_PSTATE,      gauge, GPU power state.                              # code 190
    #DCGM_FI_DEV_FAN_SPEED,   gauge, GPU fan speed (in RPM).                       # code 191

    # Temperature
    DCGM_FI_DEV_GPU_TEMP,    gauge, GPU temperature (in C).                       # code 150

    # Utilization
    DCGM_FI_DEV_GPU_UTIL,      gauge, GPU utilization (in %).                     # code 203
    #DCGM_FI_DEV_MEM_COPY_UTIL, gauge, Memory copy engine utilization (in %).      # code 204
    #DCGM_FI_DEV_ENC_UTIL,      gauge, Encoder utilization (in %).                 # code 206
    #DCGM_FI_DEV_DEC_UTIL,      gauge, Decoder utilization (in %).                 # code 207

    # PCIe / NVLink Traffic
    DCGM_FI_PROF_PCIE_TX_BYTES, counter, Total PCIe transmit bytes.                   # code 1009
    DCGM_FI_PROF_PCIE_RX_BYTES, counter, Total PCIe receive bytes.                    # code 1010
    #DCGM_FI_PROF_NVLINK_TX_BYTES, counter, Total NVLink transmitted bytes.            # code 1011
    #DCGM_FI_PROF_NVLINK_RX_BYTES, counter, Total NVLink received bytes.               # code 1012

    # Framebuffer (FB) Memory
    DCGM_FI_DEV_FB_TOTAL,        gauge, Total framebuffer memory (in MiB).          # code 250
    DCGM_FI_DEV_FB_FREE,         gauge, Free framebuffer memory (in MiB).           # code 251
    DCGM_FI_DEV_FB_USED,         gauge, Used framebuffer memory (in MiB).           # code 252
    DCGM_FI_DEV_FB_RESERVED,     gauge, Reserved framebuffer memory (in MiB).       # code 253
    DCGM_FI_DEV_FB_USED_PERCENT, gauge, Percentage of framebuffer memory used (in %). # code 254

    # ECC (Error Correcting Code)
    DCGM_FI_DEV_ECC_SBE_AGG_TOTAL, counter, Aggregate single-bit persistent ECC errors. # code 312
    DCGM_FI_DEV_ECC_DBE_AGG_TOTAL, counter, Aggregate double-bit persistent ECC errors.   # code 313

    # DCP (Dynamic Compute Partitioning) / Performance Metrics
    DCGM_FI_PROF_GR_ENGINE_ACTIVE,   gauge, Ratio of time the graphics engine is active.  # code 1001
    DCGM_FI_PROF_SM_ACTIVE,          gauge, Ratio of cycles with at least one warp active.  # code 1002
    DCGM_FI_PROF_SM_OCCUPANCY,       gauge, SM occupancy ratio (resident warps per SM).    # code 1003
    DCGM_FI_PROF_PIPE_TENSOR_ACTIVE, gauge, Ratio of cycles the tensor (HMMA) pipe is active.   # code 1004
    DCGM_FI_PROF_DRAM_ACTIVE,        gauge, Ratio of cycles the memory interface is active.   # code 1005
    #DCGM_FI_PROF_PIPE_FP64_ACTIVE,   gauge, Ratio of cycles the FP64 pipes are active.        # code 1006
    #DCGM_FI_PROF_PIPE_FP32_ACTIVE,   gauge, Ratio of cycles the FP32 pipes are active.        # code 1007
    #DCGM_FI_PROF_PIPE_FP16_ACTIVE,   gauge, Ratio of cycles the FP16 pipes are active.        # code 1008

    # P-State (GPU Power State)
    DCGM_FI_DEV_PSTATE,          gauge, GPU power state.
`
