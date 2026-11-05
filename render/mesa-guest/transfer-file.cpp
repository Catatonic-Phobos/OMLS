// Hardware transfer validation using an actual application asset, not a
// generated scene. This executable runs locally through the experimental ICD.
#include <vulkan/vulkan.h>
#include <chrono>
#include <cstring>
#include <fstream>
#include <iostream>
#include <iterator>
#include <stdexcept>
#include <thread>
#include <vector>

static void check(VkResult result,const char* operation){if(result!=VK_SUCCESS)throw std::runtime_error(std::string(operation)+": "+std::to_string(result));}
int main(int argc,char** argv){try{
    if(argc<3||argc>6)throw std::runtime_error("usage: transfer-file INPUT OUTPUT [--offset-whole] [--threaded] [--churn]");
    bool whole=false,threaded=false,churn=false;
    for(int i=3;i<argc;i++){if(std::string(argv[i])=="--offset-whole")whole=true;else if(std::string(argv[i])=="--threaded")threaded=true;else if(std::string(argv[i])=="--churn")churn=true;else throw std::runtime_error("invalid test option");}
    std::ifstream input(argv[1],std::ios::binary);if(!input)throw std::runtime_error("cannot open input");
    std::vector<char> bytes((std::istreambuf_iterator<char>(input)),{});
    if(bytes.empty()||bytes.size()>(32<<20))throw std::runtime_error("test input must be between 1 byte and 32 MiB");
    VkApplicationInfo application{VK_STRUCTURE_TYPE_APPLICATION_INFO};application.pApplicationName="OMLS real asset transfer";application.apiVersion=VK_API_VERSION_1_0;
    VkInstanceCreateInfo instanceInfo{VK_STRUCTURE_TYPE_INSTANCE_CREATE_INFO};instanceInfo.pApplicationInfo=&application;
    VkInstance instance;check(vkCreateInstance(&instanceInfo,nullptr,&instance),"vkCreateInstance");
    uint32_t count=0;check(vkEnumeratePhysicalDevices(instance,&count,nullptr),"vkEnumeratePhysicalDevices");if(count!=1)throw std::runtime_error("test requires exactly the selected remote GPU");
    VkPhysicalDevice physical;check(vkEnumeratePhysicalDevices(instance,&count,&physical),"vkEnumeratePhysicalDevices");
    VkPhysicalDeviceProperties properties;vkGetPhysicalDeviceProperties(physical,&properties);
    uint32_t extensionCount=0;check(vkEnumerateDeviceExtensionProperties(physical,nullptr,&extensionCount,nullptr),"vkEnumerateDeviceExtensionProperties");
    std::vector<VkExtensionProperties> extensions(extensionCount);check(vkEnumerateDeviceExtensionProperties(physical,nullptr,&extensionCount,extensions.data()),"vkEnumerateDeviceExtensionProperties");
    for(const auto& extension:extensions)if(strstr(extension.extensionName,"external_")||strstr(extension.extensionName,"swapchain"))throw std::runtime_error("unsupported network interop extension was advertised");
    if(churn)for(int i=0;i<80;i++){
        VkPhysicalDeviceProperties fromThread{};
        std::thread worker([&]{vkGetPhysicalDeviceProperties(physical,&fromThread);});worker.join();
        if(fromThread.deviceID!=properties.deviceID)throw std::runtime_error("shared GPU namespace failed during thread churn");
    }
    const VkDeviceSize prefix=whole?properties.limits.nonCoherentAtomSize:0;
    const VkDeviceSize copySize=(bytes.size()+3)&~VkDeviceSize(3);
    const VkDeviceSize byteCount=prefix+copySize;
    if(!strstr(properties.deviceName,"GTX 1050"))throw std::runtime_error("expected STUDIO GTX 1050");
    vkGetPhysicalDeviceQueueFamilyProperties(physical,&count,nullptr);std::vector<VkQueueFamilyProperties> families(count);vkGetPhysicalDeviceQueueFamilyProperties(physical,&count,families.data());
    uint32_t family=0;while(family<count&&!(families[family].queueFlags&VK_QUEUE_TRANSFER_BIT))++family;if(family==count)throw std::runtime_error("no transfer queue");
    float priority=1;VkDeviceQueueCreateInfo queueInfo{VK_STRUCTURE_TYPE_DEVICE_QUEUE_CREATE_INFO};queueInfo.queueFamilyIndex=family;queueInfo.queueCount=1;queueInfo.pQueuePriorities=&priority;
    VkDeviceCreateInfo deviceInfo{VK_STRUCTURE_TYPE_DEVICE_CREATE_INFO};deviceInfo.queueCreateInfoCount=1;deviceInfo.pQueueCreateInfos=&queueInfo;
    VkDevice device;check(vkCreateDevice(physical,&deviceInfo,nullptr,&device),"vkCreateDevice");VkQueue queue;vkGetDeviceQueue(device,family,0,&queue);
    VkPhysicalDeviceMemoryProperties memoryProperties;vkGetPhysicalDeviceMemoryProperties(physical,&memoryProperties);
    VkBuffer buffers[2];VkDeviceMemory memories[2];VkMemoryRequirements requirements[2];void* mappings[2];
    for(int i=0;i<2;i++){
        VkBufferCreateInfo info{VK_STRUCTURE_TYPE_BUFFER_CREATE_INFO};info.size=byteCount;info.usage=VK_BUFFER_USAGE_TRANSFER_SRC_BIT|VK_BUFFER_USAGE_TRANSFER_DST_BIT;info.sharingMode=VK_SHARING_MODE_EXCLUSIVE;
        check(vkCreateBuffer(device,&info,nullptr,&buffers[i]),"vkCreateBuffer");vkGetBufferMemoryRequirements(device,buffers[i],&requirements[i]);
        uint32_t type=0;for(;type<memoryProperties.memoryTypeCount;type++)if((requirements[i].memoryTypeBits&(1u<<type))&&(memoryProperties.memoryTypes[type].propertyFlags&VK_MEMORY_PROPERTY_HOST_VISIBLE_BIT))break;
        if(type==memoryProperties.memoryTypeCount)throw std::runtime_error("no host-visible memory");
        if(memoryProperties.memoryTypes[type].propertyFlags&VK_MEMORY_PROPERTY_HOST_COHERENT_BIT)throw std::runtime_error("network driver incorrectly advertises coherent staging RAM");
        VkMemoryAllocateInfo allocation{VK_STRUCTURE_TYPE_MEMORY_ALLOCATE_INFO};allocation.allocationSize=requirements[i].size;allocation.memoryTypeIndex=type;
        check(vkAllocateMemory(device,&allocation,nullptr,&memories[i]),"vkAllocateMemory");check(vkBindBufferMemory(device,buffers[i],memories[i],0),"vkBindBufferMemory");
        check(vkMapMemory(device,memories[i],0,requirements[i].size,0,&mappings[i]),"vkMapMemory");
    }
    memset(mappings[0],0,byteCount);memcpy(static_cast<char*>(mappings[0])+prefix,bytes.data(),bytes.size());memset(mappings[1],0xAB,byteCount);
    VkMappedMemoryRange upload{VK_STRUCTURE_TYPE_MAPPED_MEMORY_RANGE};upload.memory=memories[0];upload.offset=prefix;upload.size=whole?VK_WHOLE_SIZE:requirements[0].size;
    VkMappedMemoryRange invalid=upload;invalid.offset=256;invalid.size=VK_WHOLE_SIZE-1;
    if(vkFlushMappedMemoryRanges(device,1,&invalid)==VK_SUCCESS)throw std::runtime_error("overflowing mapped range was accepted");
    auto start=std::chrono::steady_clock::now();check(vkFlushMappedMemoryRanges(device,1,&upload),"vkFlushMappedMemoryRanges");
    VkCommandPoolCreateInfo poolInfo{VK_STRUCTURE_TYPE_COMMAND_POOL_CREATE_INFO};poolInfo.queueFamilyIndex=family;VkCommandPool pool;check(vkCreateCommandPool(device,&poolInfo,nullptr,&pool),"vkCreateCommandPool");
    VkCommandBufferAllocateInfo commandInfo{VK_STRUCTURE_TYPE_COMMAND_BUFFER_ALLOCATE_INFO};commandInfo.commandPool=pool;commandInfo.level=VK_COMMAND_BUFFER_LEVEL_PRIMARY;commandInfo.commandBufferCount=1;VkCommandBuffer command;check(vkAllocateCommandBuffers(device,&commandInfo,&command),"vkAllocateCommandBuffers");
    VkCommandBufferBeginInfo begin{VK_STRUCTURE_TYPE_COMMAND_BUFFER_BEGIN_INFO};check(vkBeginCommandBuffer(command,&begin),"vkBeginCommandBuffer");
    VkBufferCopy region{prefix,prefix,copySize};vkCmdCopyBuffer(command,buffers[0],buffers[1],1,&region);
    VkMemoryBarrier barrier{VK_STRUCTURE_TYPE_MEMORY_BARRIER};barrier.srcAccessMask=VK_ACCESS_TRANSFER_WRITE_BIT;barrier.dstAccessMask=VK_ACCESS_HOST_READ_BIT;
    vkCmdPipelineBarrier(command,VK_PIPELINE_STAGE_TRANSFER_BIT,VK_PIPELINE_STAGE_HOST_BIT,0,1,&barrier,0,nullptr,0,nullptr);
    check(vkEndCommandBuffer(command),"vkEndCommandBuffer");VkFenceCreateInfo fenceInfo{VK_STRUCTURE_TYPE_FENCE_CREATE_INFO};VkFence fence;check(vkCreateFence(device,&fenceInfo,nullptr,&fence),"vkCreateFence");
    VkSubmitInfo submit{VK_STRUCTURE_TYPE_SUBMIT_INFO};submit.commandBufferCount=1;submit.pCommandBuffers=&command;
    auto submitAndWait=[&]{check(vkQueueSubmit(queue,1,&submit,fence),"vkQueueSubmit");check(vkWaitForFences(device,1,&fence,VK_TRUE,15000000000ull),"vkWaitForFences");};
    if(threaded){std::exception_ptr error;std::thread worker([&]{try{submitAndWait();}catch(...){error=std::current_exception();}});worker.join();if(error)std::rethrow_exception(error);}else submitAndWait();
    VkMappedMemoryRange download{VK_STRUCTURE_TYPE_MAPPED_MEMORY_RANGE};download.memory=memories[1];download.offset=prefix;download.size=whole?VK_WHOLE_SIZE:requirements[1].size;
    check(vkInvalidateMappedMemoryRanges(device,1,&download),"vkInvalidateMappedMemoryRanges");
    if(memcmp(static_cast<char*>(mappings[1])+prefix,bytes.data(),bytes.size()))throw std::runtime_error("GPU readback differs from original asset");
    std::ofstream output(argv[2],std::ios::binary);output.write(static_cast<const char*>(mappings[1])+prefix,bytes.size());output.close();if(!output)throw std::runtime_error("cannot publish readback file");
    const auto milliseconds=std::chrono::duration_cast<std::chrono::milliseconds>(std::chrono::steady_clock::now()-start).count();
    vkDestroyFence(device,fence,nullptr);vkDestroyCommandPool(device,pool,nullptr);
    for(int i=0;i<2;i++){vkUnmapMemory(device,memories[i]);vkDestroyBuffer(device,buffers[i],nullptr);vkFreeMemory(device,memories[i],nullptr);}
    vkDestroyDevice(device,nullptr);vkDestroyInstance(instance,nullptr);
    std::cout<<"{\"bytes\":"<<bytes.size()<<",\"offset\":"<<prefix<<",\"whole_size\":"<<(whole?"true":"false")<<",\"cross_thread_submit\":"<<(threaded?"true":"false")<<",\"temporary_threads\":"<<(churn?80:0)<<",\"overflow_rejected\":true,\"upload_copy_readback_ms\":"<<milliseconds<<",\"verified\":true,\"application_rendering_ready\":false}\n";
    return 0;
}catch(const std::exception& e){std::cerr<<e.what()<<"\n";return 1;}}
