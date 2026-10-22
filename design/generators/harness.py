import json,sys,os
S=sys.argv[1]; P=f'{S}/ds/project'; t=json.load(open(f'{P}/tokens.json'))
def block(theme):
    out=[f"--{tk['name']}:{tk['value'].get(theme, tk['value']['light'])};" for tk in t['color']['tokens']]
    for fam in ['spacing','radius','shadow','size','zIndex']:
        for tk in t[fam]['tokens']:
            v=tk['value']; v=v.get(theme, v['light']) if isinstance(v,dict) else v; out.append(f"--{tk['name']}:{v};")
    out+= [f"--font-{k}:{v};" for k,v in t['type']['families'].items()]
    return ''.join(out)
css=f":root{{{block('light')}}}\n:root[data-theme=dark]{{{block('dark')}}}\nbody{{margin:0;background:var(--bg);color:var(--ink);font-family:var(--font-sans)}}\n"
bundle=open(f'{P}/components/bundle.css').read()
for comp in os.listdir(f'{P}/components'):
    pf=f'{P}/components/{comp}/preview.html'
    if not os.path.exists(pf): continue
    h=open(pf).read()
    for th in ['light','dark']:
        x=h.replace('<html lang="en">',f'<html lang="en" data-theme="{th}">').replace('<head>','<head><style>'+css+'</style><style>'+bundle+'</style>',1)
        open(f'{S}/harness/{comp}-{th}.html','w').write(x)
print('harness ok')
