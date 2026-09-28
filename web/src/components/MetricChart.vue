<script setup lang="ts">
import { computed } from 'vue'
import VChart from 'vue-echarts'
import { use } from 'echarts/core'
import { LineChart, BarChart } from 'echarts/charts'
import { GridComponent, TooltipComponent, LegendComponent } from 'echarts/components'
import { CanvasRenderer } from 'echarts/renderers'
use([LineChart,BarChart,GridComponent,TooltipComponent,LegendComponent,CanvasRenderer])
const props=defineProps<{series:any[],height?:number,kind?:'line'|'bar',unit?:string,categories?:string[]}>()
const colors=['#2443B5','#E07A1F','#13866F','#8B3FB8']
const option=computed(()=>({color:colors,animation:false,grid:{left:48,right:18,top:16,bottom:30},tooltip:{trigger:'axis'},legend:{show:false},xAxis:{type:'category',boundaryGap:props.kind==='bar',data:props.categories||props.series?.[0]?.points?.map((p:any)=>new Date(p.time).toLocaleTimeString('zh-CN',{hour:'2-digit',minute:'2-digit'}))||[],axisLine:{lineStyle:{color:'#E4E7EC'}},axisTick:{show:false},axisLabel:{color:'#8A92A2',fontFamily:'IBM Plex Mono',fontSize:10}},yAxis:{type:'value',axisLine:{show:false},axisLabel:{color:'#8A92A2',fontFamily:'IBM Plex Mono',fontSize:10,formatter:(v:number)=>`${v}${props.unit==='%'?'%':''}`},splitLine:{lineStyle:{color:'#EEF0F3',type:'dashed'}}},series:props.series.map((s:any)=>({name:s.name,type:props.kind||'line',data:s.points?.map((p:any)=>p.value)||s.data||[],smooth:true,showSymbol:false,symbolSize:5,barMaxWidth:28,areaStyle:props.kind==='bar'?undefined:{opacity:.06},lineStyle:{width:2}}))}))
</script><template><v-chart :option="option" autoresize :style="{height:(height||220)+'px',width:'100%'}"/></template>
