<template>
  <div>
    <!-- Drop zone -->
    <div
      ref="dropZone"
      class="border-2 border-dashed rounded-xl text-center cursor-pointer transition-all duration-200"
      :class="[
        isDragging
          ? 'border-indigo-500 bg-indigo-500/10 scale-[1.01]'
          : 'border-dark-600 hover:border-dark-400 hover:bg-dark-800/50',
        files.length > 0 ? 'p-3' : 'p-6'
      ]"
      @dragover.prevent="isDragging = true"
      @dragleave.prevent="isDragging = false"
      @drop.prevent="onDrop"
      @click="$refs.fileInput.click()"
    >
      <input
        ref="fileInput"
        type="file"
        multiple
        class="hidden"
        :accept="accept"
        @change="onFileSelect"
      />
      <div class="flex flex-col items-center gap-1.5">
        <BaseIcon
          :name="isDragging ? 'arrow-down-tray' : 'paper-clip'"
          :class="files.length > 0 ? 'h-5 w-5' : 'h-8 w-8'"
          class="text-dark-400"
        />
        <span class="text-sm text-dark-300">
          {{ isDragging ? '松开即可上传' : '拖拽文件到这里，或点击选择' }}
        </span>
        <span v-if="files.length === 0" class="text-xs text-dark-500">最大 25MB / 文件</span>
      </div>
    </div>

    <!-- File list -->
    <div v-if="files.length" class="mt-3 space-y-2">
      <div
        v-for="f in files"
        :key="f.id || f.name"
        class="flex items-center gap-3 bg-dark-800/80 rounded-lg px-3 py-2.5 transition-all"
      >
        <!-- Preview -->
        <div class="flex-shrink-0 w-10 h-10 rounded-md overflow-hidden bg-dark-700 flex items-center justify-center">
          <img
            v-if="f.previewUrl"
            :src="f.previewUrl"
            class="w-full h-full object-cover"
            @error="f.previewUrl = null"
          />
          <BaseIcon v-else :name="getFileIcon(f.mimeType || f.type)" class="h-5 w-5 text-dark-400" />
        </div>

        <!-- Info -->
        <div class="flex-1 min-w-0">
          <div class="text-sm text-dark-200 truncate" :title="f.name">{{ f.name }}</div>
          <div class="flex items-center gap-2 text-xs text-dark-400 mt-0.5">
            <span>{{ formatSize(f.size) }}</span>
            <span v-if="f.status === 'uploading'" class="text-amber-400 flex items-center gap-1">
              <BaseIcon name="arrow-path" class="h-3 w-3 animate-spin" />
              <span>上传中...</span>
            </span>
            <span v-else-if="f.status === 'done'" class="text-green-400 flex items-center gap-1">
              <BaseIcon name="check" class="h-3 w-3" />
              <span>完成</span>
            </span>
            <span v-else-if="f.status === 'error'" class="text-red-400 flex items-center gap-1">
              <BaseIcon name="x-mark" class="h-3 w-3" />
              <span>{{ f.error || '失败' }}</span>
            </span>
          </div>
        </div>

        <!-- Remove -->
        <button
          type="button"
          @click.stop="removeFile(f)"
          class="flex-shrink-0 w-7 h-7 rounded-full flex items-center justify-center text-dark-400 hover:text-dark-200 hover:bg-dark-700 transition-colors disabled:opacity-30"
          :disabled="f.status === 'uploading'"
          aria-label="移除文件"
        >
          <BaseIcon name="x-mark" class="h-4 w-4" />
        </button>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, watch } from 'vue'
import { uploadFiles, deleteUpload, getUploadPreviewUrl } from '@/api'
import BaseIcon from '@/components/BaseIcon.vue'

const props = defineProps({
  accept: {
    type: String,
    default: '.pdf,.doc,.docx,.xls,.xlsx,.ppt,.pptx,.txt,.csv,.zip,.rar,.7z,.jpg,.jpeg,.png,.gif,.bmp,.svg,.mp3,.mp4,.avi,.mov'
  }
})

const emit = defineEmits(['update:attachments'])

const isDragging = ref(false)
const files = ref([])
const dropZone = ref(null)
// P0-7：上传中计数器，正确管理 isUploading 状态
const uploadingCount = ref(0)

function getAttachmentIds() {
  return files.value
    .filter(f => f.status === 'done' && f.attId)
    .map(f => f.attId)
}

function getFiles() {
  return files.value.map(f => f._file)
}

// P0-7：返回真实上传状态（修复原始终返回 false 的 bug）
function isUploading() {
  return uploadingCount.value > 0
}

defineExpose({ getAttachmentIds, getFiles, isUploading })

// 返回 BaseIcon name（小写下划线命名）
function getFileIcon(mime) {
  if (!mime) return 'document'
  if (mime.startsWith('image/')) return 'photo'
  if (mime.startsWith('video/')) return 'film'
  if (mime.startsWith('audio/')) return 'musical-note'
  if (mime.includes('pdf')) return 'document-text'
  if (mime.includes('zip') || mime.includes('rar') || mime.includes('7z')) return 'archive-box'
  if (mime.includes('word') || mime.includes('document')) return 'document-text'
  if (mime.includes('excel') || mime.includes('sheet')) return 'table-cells'
  if (mime.includes('powerpoint') || mime.includes('presentation')) return 'presentation-chart-bar'
  if (mime.startsWith('text/') || mime.includes('json')) return 'document-text'
  return 'document'
}

function formatSize(bytes) {
  if (bytes < 1024) return bytes + ' B'
  if (bytes < 1024 * 1024) return (bytes / 1024).toFixed(1) + ' KB'
  return (bytes / (1024 * 1024)).toFixed(1) + ' MB'
}

function isImage(mime) {
  return mime && mime.startsWith('image/')
}

function onDrop(e) {
  isDragging.value = false
  const dropped = Array.from(e.dataTransfer.files)
  if (dropped.length) handleFiles(dropped)
}

function onFileSelect(e) {
  const selected = Array.from(e.target.files)
  if (selected.length) handleFiles(selected)
  e.target.value = ''
}

async function handleFiles(newFiles) {
  for (const file of newFiles) {
    if (file.size > 25 * 1024 * 1024) continue
    const item = {
      name: file.name,
      size: file.size,
      type: file.type,
      mimeType: file.type,
      status: 'uploading',
      progress: 0,
      attId: null,
      previewUrl: null,
      error: null,
      _file: file,
    }
    if (isImage(file.type)) {
      item.previewUrl = URL.createObjectURL(file)
    }
    files.value.push(item)
    // P0-7：并行上传，但用 uploadingCount 跟踪
    uploadOne(item)
  }
}

// P0-7：正确管理 uploading 状态（uploading → done/error），用 uploadingCount 跟踪
async function uploadOne(item) {
  uploadingCount.value++
  try {
    const fd = new FormData()
    fd.append('files', item._file)
    const res = await uploadFiles(fd)
    if (res.files && res.files.length) {
      item.attId = res.files[0].id
      if (isImage(item.mimeType)) {
        item.previewUrl = getUploadPreviewUrl(res.files[0].id)
      }
    }
    item.status = 'done'
    item.progress = 100
  } catch (e) {
    item.status = 'error'
    item.error = e.message || '上传失败'
  } finally {
    uploadingCount.value--
    emitUpdate()
  }
}

async function removeFile(file) {
  if (file.status === 'uploading') return
  if (file.attId) {
    try { await deleteUpload(file.attId) } catch {}
  }
  if (file.previewUrl && file.previewUrl.startsWith('blob:')) {
    URL.revokeObjectURL(file.previewUrl)
  }
  files.value = files.value.filter(f => f !== file)
  emitUpdate()
}

function emitUpdate() {
  emit('update:attachments', getAttachmentIds())
}
</script>
