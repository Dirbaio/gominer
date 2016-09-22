/*
 * Copyright 2011-2012 Con Kolivas
 *
 * This program is free software; you can redistribute it and/or modify it
 * under the terms of the GNU General Public License as published by the Free
 * Software Foundation; either version 3 of the License, or (at your option)
 * any later version.  See COPYING for more details.
 */

#include <stddef.h>
#include <stdbool.h>
#include <stdlib.h>
#include <stdio.h>
#include "adl_sdk.h"
#include "adl_functions.h"

#define MAX_GPUDEVICES 16

// declarations in adl_functions.h for these are formatted for dynamic loading
int ADL_Adapter_AdapterInfo_Get(LPAdapterInfo lpInfo, int iInputSize);
int ADL_Adapter_ID_Get(int iAdapterIndex, int *lpAdapterID);
int ADL_Adapter_NumberOfAdapters_Get(int *lpNumAdapters);
int ADL_Main_Control_Create(ADL_MAIN_MALLOC_CALLBACK callback, int iEnumConnectedAdapters);
int ADL_Overdrive5_FanSpeed_Get(int iAdapterIndex, int iThermalControllerIndex, ADLFanSpeedValue *lpFanSpeedValue);
int ADL_Overdrive5_Temperature_Get (int iAdapterIndex, int iThermalControllerIndex, ADLTemperature *lpTemperature);

int getADLInfo(int deviceid, char field[64]);

struct gpu_adapters {
  int iAdapterIndex;
  int iBusNumber;
  int virtual_gpu;
  int id;
};

// Memory allocation function
static void * __stdcall ADL_Main_Memory_Alloc(int iSize)
{
  void *lpBuffer = malloc(iSize);

  return lpBuffer;
}

// Optional Memory de-allocation function
static void __stdcall ADL_Main_Memory_Free (void **lpBuffer)
{
  if (*lpBuffer != NULL) {
    free (*lpBuffer);
    *lpBuffer = NULL;
  }
}

int getADLFanPercent(int deviceid) {
  int fanPercent = 0;
  fanPercent = getADLInfo(deviceid, "fanPercent");
  return fanPercent;
}

int getADLTemp(int deviceid) {
  int temp = 0;
  temp = getADLInfo(deviceid, "temp");
  return temp;
}

int getADLInfo(int deviceid, char field[64]) {
  int result, i, j, devices = 0, last_adapter = -1, gpu = 0, dummy = 0;
  int iNumberAdapters;
  struct gpu_adapters adapters[MAX_GPUDEVICES], vadapters[MAX_GPUDEVICES];
  bool devs_match = true;
  ADLBiosInfo BiosInfo;
  LPAdapterInfo lpInfo = NULL;

  if (ADL_OK != ADL_Main_Control_Create(ADL_Main_Memory_Alloc, 1)) {
    return 0;
  }

  // Obtain the number of adapters for the system
  result = ADL_Adapter_NumberOfAdapters_Get(&iNumberAdapters);
  if (result != ADL_OK) {
    return 0;
  }

  if (iNumberAdapters > 0) {
    lpInfo = (LPAdapterInfo)malloc(sizeof (AdapterInfo) * iNumberAdapters);
    memset ( lpInfo,'\0', sizeof (AdapterInfo) * iNumberAdapters );

    lpInfo->iSize = sizeof(lpInfo);
    // Get the AdapterInfo structure for all adapters in the system
    result = ADL_Adapter_AdapterInfo_Get (lpInfo, sizeof (AdapterInfo) * iNumberAdapters);
    if (result != ADL_OK) {
      return 0;
    }
  } else {
    return 0;
  }

  /* Iterate over iNumberAdapters and find the lpAdapterID of real devices */
  for (i = 0; i < iNumberAdapters; i++) {
    int iAdapterIndex;
    int lpAdapterID;

    iAdapterIndex = lpInfo[i].iAdapterIndex;

    /* Get unique identifier of the adapter, 0 means not AMD */
    result = ADL_Adapter_ID_Get(iAdapterIndex, &lpAdapterID);

    if (result != ADL_OK) {
      continue;
    }

    /* Each adapter may have multiple entries */
    if (lpAdapterID == last_adapter) {
      continue;
    }

    adapters[devices].iAdapterIndex = iAdapterIndex;
    adapters[devices].iBusNumber = lpInfo[i].iBusNumber;
    adapters[devices].id = i;

    if (deviceid == devices) {
      if (strcmp(field, "fanPercent") == 0) {
        ADLFanSpeedValue lpFanSpeedValue = {0};
        lpFanSpeedValue.iSize = sizeof(ADLFanSpeedValue);
        lpFanSpeedValue.iSpeedType = ADL_DL_FANCTRL_SPEED_TYPE_PERCENT;
        if (ADL_OK != ADL_Overdrive5_FanSpeed_Get(iAdapterIndex, 0, &lpFanSpeedValue)) {
          return 0;
        }
        return lpFanSpeedValue.iFanSpeed;
      }
      if (strcmp(field, "temp") == 0) {
        ADLTemperature lpTemperature = {0};
        lpTemperature.iSize = sizeof(ADLTemperature);
        lpTemperature.iTemperature = 0;
        if (ADL_OK != ADL_Overdrive5_Temperature_Get(iAdapterIndex, 0, &lpTemperature)) {
          return 0;
        }
        return lpTemperature.iTemperature;
      }
    }

    devices++;
    last_adapter = lpAdapterID;

    if (!lpAdapterID) {
      continue;
    }
  }

  return 0;
}
