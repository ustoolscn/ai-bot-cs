import { ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { request } from '@/api/client'

export function useBulkDelete(reload: () => Promise<void>) {
  const selectedIds = ref<string[]>([])
  const deleting = ref(false)
  async function deleteSelected(kind: string, label: string){
    const ids=[...selectedIds.value]
    if (!ids.length || deleting.value) return
    try {await ElMessageBox.confirm('确认永久删除选中的 '+ids.length+' 条'+label+'？','批量删除',{type:'warning',confirmButtonText:'删除',cancelButtonText:'取消'})} catch {return}
    deleting.value=true
    try{
      const result=await request<{deleted:number}>({url:'/learning/'+kind+'/bulk-delete',method:'POST',data:{ids}})
      selectedIds.value=[]
      ElMessage.success('已删除 '+result.deleted+' 条记录')
      await reload()
    }catch(e){ElMessage.error(e instanceof Error?e.message:'批量删除失败')}
    finally{deleting.value=false}
  }
  return { selectedIds, deleting, deleteSelected }
}
