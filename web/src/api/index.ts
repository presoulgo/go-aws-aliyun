import axios from 'axios'
import { ElMessage } from 'element-plus'
export const api = axios.create({ baseURL: '/api/v1', timeout: 30000 })
api.interceptors.request.use(config => { const token=localStorage.getItem('token'); if(token)config.headers.Authorization=`Bearer ${token}`;return config })
api.interceptors.response.use(r=>r.data,err=>{if(err.response?.status===401&&location.pathname!=='/login'){localStorage.removeItem('token');location.href='/login'}else if(err.response?.data?.error){ElMessage.error(err.response.data.error)}return Promise.reject(err)})
export const get = (path:string, params?:any):Promise<any> => api.get(path,{params}) as any
export const post = (path:string,data?:any):Promise<any> => api.post(path,data) as any
export const put = (path:string,data?:any):Promise<any> => api.put(path,data) as any
export const patch = (path:string,data?:any):Promise<any> => api.patch(path,data) as any
export const del = (path:string):Promise<any> => api.delete(path) as any
